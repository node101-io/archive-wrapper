package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

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
}

type BlockNotification struct {
	Height int64 `json:"height"`
}

func NewIndexer(
	conn *pgx.Conn,
	client *fetchmina.MinaClient,
	db *database.DbManager,
	startBlockHeight int64,
	confirmationDepth int64,
	ctx context.Context,
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

	var startingBlockHeight int64

	exists, err := db.HasBlockHeight()
	if err != nil {
		return nil, err
	}

	if exists {
		startingBlockHeight, err = db.GetBlockHeight()
		if err != nil {
			return nil, err
		}
	} else {
		startingBlockHeight = startBlockHeight - 1
	}

	minaBlockHeight, err := client.GetMinaBlockHeight(ctx)
	if err != nil {
		return nil, err
	}

	// This loop is for catching up with Mina.
	// Will be useful for when for-some-reason wrapper is restarted.
	// This loop will catch up with the actions sent to contract when the wrapper wasn't working

	for i := startingBlockHeight + 1; i <= minaBlockHeight-confirmationDepth; i++ {
		actions, err := client.FetchActions(ctx, int(i))
		if err != nil {
			return nil, err
		}

		if len(actions) == 0 {
			if err := db.InsertBlockHeight(i); err != nil {
				return nil, err
			}
			continue
		}

		record, err := IndexActions(actions, i)
		if err != nil {
			return nil, err
		}

		if err := db.Insert(record); err != nil {
			return nil, err
		}

		if err := db.InsertBlockHeight(i); err != nil {
			return nil, err
		}
	}

	return &Indexer{
		client:            client,
		conn:              conn,
		db:                db,
		confirmationDepth: confirmationDepth,
	}, nil
}

func (indexer *Indexer) Run(ctx context.Context) error {

	defer indexer.conn.Close(ctx)

	_, err := indexer.conn.Exec(ctx, "LISTEN blocks_inserted")
	if err != nil {
		return fmt.Errorf("listen blocks_inserted: %w", err)
	}

	for {
		notification, err := indexer.conn.WaitForNotification(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			return fmt.Errorf("wait for notification: %w", err)
		}

		var msg BlockNotification
		if err := json.Unmarshal([]byte(notification.Payload), &msg); err != nil {
			log.Printf("invalid notification payload %q: %v", notification.Payload, err)
			continue
		}

		lastCanonicalBlock := msg.Height - indexer.confirmationDepth
		if err := indexer.indexAvailableBlocks(ctx, lastCanonicalBlock); err != nil {
			return err
		}
	}
}

func (indexer *Indexer) indexAvailableBlocks(ctx context.Context, height int64) error {

	actions, err := indexer.client.FetchActions(ctx, int(height))
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
