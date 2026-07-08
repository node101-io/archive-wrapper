package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/node101-io/archive-wrapper/apperrors"
	"github.com/node101-io/archive-wrapper/database"
	fetchmina "github.com/node101-io/archive-wrapper/fetchmina"
)

type Indexer struct {
	conn              *pgx.Conn
	client            *fetchmina.MinaClient
	db                *database.DbManager
	confirmationDepth int64
	startBlockHeight  int64
}

type BlockNotification struct {
	Height int64 `json:"height"`
}

const (
	maxRetries = 3
	retryDelay = 2 * time.Second
)

func NewIndexer(
	conn *pgx.Conn,
	client *fetchmina.MinaClient,
	db *database.DbManager,
	startBlockHeight int64,
	confirmationDepth int64,
) (*Indexer, error) {

	if conn == nil {
		return nil, apperrors.ErrNilConnection
	}

	if client == nil {
		return nil, apperrors.ErrNilMinaClient
	}

	if err := db.Validate(); err != nil {
		return nil, err
	}

	if startBlockHeight <= 0 {
		return nil, apperrors.ErrBlockHeightMustBeBiggerThanZero
	}

	if confirmationDepth <= 0 {
		return nil, apperrors.ErrInvalidBlockRange
	}

	return &Indexer{
		client:            client,
		conn:              conn,
		db:                db,
		confirmationDepth: confirmationDepth,
		startBlockHeight:  startBlockHeight,
	}, nil
}

func (indexer *Indexer) Sync(ctx context.Context) error {
	if indexer == nil {
		return apperrors.ErrNilIndexer
	}

	minaBlockHeight, err := indexer.client.GetMinaBlockHeight(ctx)
	if err != nil {
		return err
	}

	return indexer.syncTo(
		ctx,
		minaBlockHeight-indexer.confirmationDepth,
	)
}

func (indexer *Indexer) syncTo(
	ctx context.Context,
	target int64,
) error {
	cursor := indexer.startBlockHeight - 1

	exists, err := indexer.db.HasBlockHeight()
	if err != nil {
		return err
	}

	if exists {
		cursor, err = indexer.db.GetBlockHeight()
		if err != nil {
			return err
		}
	}

	if target <= cursor {
		return nil
	}

	for height := cursor + 1; height <= target; height++ {
		height := height

		if err := withRetry(ctx, func() error {
			return indexer.indexAvailableBlocks(ctx, height)
		}); err != nil {
			return fmt.Errorf("index block %d: %w", height, err)
		}
	}

	return nil
}

func (indexer *Indexer) Run(ctx context.Context) error {

	// 1. Önce notification aboneliğini başlat.
	if _, err := indexer.conn.Exec(
		ctx,
		"LISTEN blocks_inserted",
	); err != nil {
		return err
	}

	// 2. Persisted cursor'ı yükle ve mevcut tip'e kadar catch-up yap.
	if err := indexer.Sync(ctx); err != nil {
		return fmt.Errorf("initial sync: %w", err)
	}

	// 3. Yeni notification'ları takip et.
	for {
		notification, err := indexer.conn.WaitForNotification(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("wait for notification: %w", err)
		}

		var msg BlockNotification
		if err := json.Unmarshal(
			[]byte(notification.Payload),
			&msg,
		); err != nil {
			return err
		}

		target := msg.Height - indexer.confirmationDepth

		// Eski notification ise no-op.
		// Arada eksik block varsa tamamını işler.
		if err := indexer.syncTo(ctx, target); err != nil {
			return err
		}
	}
}

// withRetry retries fn up to maxRetries times with a fixed delay between
// attempts. It stops early if ctx is cancelled.
func withRetry(ctx context.Context, fn func() error) error {
	var err error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err = fn(); err == nil {
			return nil
		}

		if attempt == maxRetries {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay):
		}
	}

	return err
}

func (indexer *Indexer) indexAvailableBlocks(ctx context.Context, height int64) error {

	actions, err := indexer.client.FetchActions(ctx, height)
	if err != nil {
		return err
	}

	if len(actions) == 0 {
		return indexer.db.InsertBlockHeight(height)
	}

	record, err := IndexActions(actions, height)
	if err != nil {
		return err
	}

	if err := indexer.db.Insert(record); err != nil {
		return err
	}

	return indexer.db.InsertBlockHeight(height)
}
