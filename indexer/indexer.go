package indexer

import (
	"context"
	"time"

	"archive-wrapper/database"
	"archive-wrapper/errors"
	fetchmina "archive-wrapper/fetch_mina"
)

type Indexer struct {
	client                 *fetchmina.MinaClient
	db                     *database.DbManager
	lastIndexedBlockHeight int64
	blockBatchSize         int
	interval               time.Duration
}

func NewIndexer(
	client *fetchmina.MinaClient,
	db *database.DbManager,
	startBlockHeight int64,
	blockBatchSize int,
	interval time.Duration,
) (*Indexer, error) {

	if client == nil {
		return nil, errors.ErrNilMinaClient
	}

	if err := db.Validate(); err != nil {
		return nil, err
	}

	if startBlockHeight < 0 {
		return nil, errors.ErrBlockHeightMustBeBiggerThanZero
	}

	if blockBatchSize <= 0 {
		return nil, errors.ErrInvalidBlockRange
	}

	if interval <= 0 {
		return nil, errors.ErrInvalidBlockRange
	}

	return &Indexer{
		client:                 client,
		db:                     db,
		lastIndexedBlockHeight: startBlockHeight - 1,
		blockBatchSize:         blockBatchSize,
		interval:               interval,
	}, nil
}

func (indexer *Indexer) Run(ctx context.Context) error {

	if err := indexer.indexAvailableBlocks(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(indexer.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			if err := indexer.indexAvailableBlocks(ctx); err != nil {
				return err
			}
		}
	}
}

func (indexer *Indexer) indexAvailableBlocks(ctx context.Context) error {

	latestBlockHeight, err := indexer.client.GetMinaBlockHeight(ctx)
	if err != nil {
		return err
	}

	nextBlockHeight := indexer.lastIndexedBlockHeight + 1
	if nextBlockHeight > latestBlockHeight {
		return nil
	}

	endBlockHeight := nextBlockHeight + int64(indexer.blockBatchSize) - 1
	if endBlockHeight > latestBlockHeight {
		endBlockHeight = latestBlockHeight
	}

	actions, err := indexer.client.FetchActions(ctx, int(nextBlockHeight), int(endBlockHeight))
	if err != nil {
		return err
	}

	records := IndexActions(actions)
	for _, record := range records {
		if err := indexer.db.Insert(record); err != nil {
			return err
		}
	}

	if err := indexer.db.InsertBlockHeight(endBlockHeight); err != nil {
		return err
	}

	indexer.lastIndexedBlockHeight = endBlockHeight
	return nil
}
