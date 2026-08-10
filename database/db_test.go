package database

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/node101-io/archive-wrapper/actions"
	"github.com/node101-io/archive-wrapper/apperrors"

	proto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	leveldbErrors "github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

const blockHeightDatabaseKey = "db-key"

func TestDbManager_InsertThenGet(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)

	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	want := actions.DbRecord{
		Key: 7,
		Actions: []*actions.Action{
			{
				BlockHeight: 7,
				XCoordinate: []byte("alice-x"),
				IsOdd:       true,
				ActionType:  actions.ActionType_DEPOSIT,
				Amount:      42,
			},
		},
	}

	err = manager.Insert(want)
	require.NoError(t, err)

	got, err := manager.Get(want.Key)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, got.Key, want.Key)
	require.Equal(t, len(got.Actions), 1) // Because we inserted only 1 action

	// Ensure they have the same values
	require.Equal(t, got.Actions[0].BlockHeight, want.Actions[0].BlockHeight)
	require.Equal(t, got.Actions[0].XCoordinate, want.Actions[0].XCoordinate)
	require.Equal(t, got.Actions[0].IsOdd, want.Actions[0].IsOdd)
	require.Equal(t, got.Actions[0].ActionType, want.Actions[0].ActionType)
	require.Equal(t, got.Actions[0].Amount, want.Actions[0].Amount)

}

func TestDbManagerInsertBlockHeight(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	var currentHeight int64 = 8

	err = manager.InsertBlockHeight(currentHeight)
	require.NoError(t, err)

	got, err := manager.GetBlockHeight()
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, got, currentHeight)

	currentHeight += 1
	err = manager.InsertBlockHeight(currentHeight)
	require.NoError(t, err)

	got, err = manager.GetBlockHeight()
	require.NoError(t, err)
	require.NotNil(t, got)

}
func TestDbManagerInsertBlockHeightRejectsRegression(t *testing.T) {

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NotNil(t, logger)

	manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	require.NotNil(t, manager)

	defer func() {
		require.NoError(t, manager.Close())
	}()

	require.NoError(t, manager.InsertBlockHeight(10))

	err = manager.InsertBlockHeight(9)
	require.Error(t, err)
	require.True(t, errors.Is(err, apperrors.ErrBlockHeightRegression))
}

func TestDbManagerInitializeOrValidateDeploymentIsIdempotent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := filepath.Join(t.TempDir(), "wrapper-db")
	metadata := testDeploymentMetadata()

	manager, err := NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	state, err := manager.InitializeOrValidateDeployment("archive-wrapper:deployment", metadata)
	require.NoError(t, err)
	require.Equal(t, DeploymentStateFresh, state)
	require.NoError(t, manager.InsertBlockHeight(10))
	require.NoError(t, manager.Close())

	manager, err = NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, manager.Close())
	}()

	state, err = manager.InitializeOrValidateDeployment("archive-wrapper:deployment", metadata)
	require.NoError(t, err)
	require.Equal(t, DeploymentStateInitialized, state)
	height, err := manager.GetBlockHeight()
	require.NoError(t, err)
	require.Equal(t, int64(10), height)
}

func TestDbManagerInitializeOrValidateDeploymentAcceptsExistingEmptyDirectory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() { require.NoError(t, manager.Close()) }()

	state, err := manager.InitializeOrValidateDeployment("archive-wrapper:deployment", testDeploymentMetadata())
	require.NoError(t, err)
	require.Equal(t, DeploymentStateFresh, state)
}

func TestDbManagerInitializeOrValidateDeploymentRejectsMismatch(t *testing.T) {
	want := testDeploymentMetadata()
	tests := []struct {
		name   string
		mutate func(*DeploymentMetadata)
	}{
		{
			name: "schema version",
			mutate: func(metadata *DeploymentMetadata) {
				metadata.SchemaVersion++
			},
		},
		{
			name: "Mina network",
			mutate: func(metadata *DeploymentMetadata) {
				metadata.MinaNetworkID = "mainnet"
			},
		},
		{
			name: "contract address",
			mutate: func(metadata *DeploymentMetadata) {
				metadata.ContractAddress = "B62qDifferentContract"
			},
		},
		{
			name: "start height",
			mutate: func(metadata *DeploymentMetadata) {
				metadata.StartHeight++
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
			require.NoError(t, err)
			defer func() {
				require.NoError(t, manager.Close())
			}()

			state, err := manager.InitializeOrValidateDeployment("archive-wrapper:deployment", want)
			require.NoError(t, err)
			require.Equal(t, DeploymentStateFresh, state)

			got := want
			tt.mutate(&got)
			_, err = manager.InitializeOrValidateDeployment("archive-wrapper:deployment", got)
			require.ErrorIs(t, err, apperrors.ErrDeploymentMetadataMismatch)
		})
	}
}

func TestDbManagerInitializeOrValidateDeploymentRejectsStateWithoutMetadata(t *testing.T) {
	tests := []struct {
		name  string
		key   []byte
		value []byte
	}{
		{name: "cursor", key: []byte(blockHeightDatabaseKey), value: encodeBlockHeight(10)},
		{name: "block record", key: encodeBlockHeight(10), value: []byte("record")},
		{name: "unknown application key", key: []byte("unknown-key"), value: []byte("value")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
			require.NoError(t, err)
			defer func() { require.NoError(t, manager.Close()) }()
			require.NoError(t, manager.db.Put(tt.key, tt.value, nil))

			state, err := manager.InitializeOrValidateDeployment("archive-wrapper:deployment", testDeploymentMetadata())
			require.ErrorIs(t, err, apperrors.ErrDBStateIncomplete)
			require.Equal(t, DeploymentStateUnspecified, state)
		})
	}
}

func TestDbManagerInitializeOrValidateDeploymentAllowsInitializedDatabaseWithoutCursor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := t.TempDir()
	manager, err := NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	state, err := manager.InitializeOrValidateDeployment(
		"archive-wrapper:deployment",
		testDeploymentMetadata(),
	)
	require.NoError(t, err)
	require.Equal(t, DeploymentStateFresh, state)
	require.NoError(t, manager.Close())

	manager, err = NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, manager.Close())
	}()

	state, err = manager.InitializeOrValidateDeployment(
		"archive-wrapper:deployment",
		testDeploymentMetadata(),
	)
	require.NoError(t, err)
	require.Equal(t, DeploymentStateInitialized, state)
	hasCursor, err := manager.HasBlockHeight()
	require.NoError(t, err)
	require.False(t, hasCursor)
}

func TestDbManagerInitializeOrValidateDeploymentValidatesCursorStartBoundary(t *testing.T) {
	tests := []struct {
		name        string
		cursor      int64
		wantCorrupt bool
	}{
		{name: "below start height", cursor: 9, wantCorrupt: true},
		{name: "at start height", cursor: 10},
		{name: "above start height", cursor: 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
			require.NoError(t, err)
			defer func() { require.NoError(t, manager.Close()) }()

			metadata := testDeploymentMetadata()
			state, err := manager.InitializeOrValidateDeployment("archive-wrapper:deployment", metadata)
			require.NoError(t, err)
			require.Equal(t, DeploymentStateFresh, state)
			require.NoError(t, manager.InsertBlockHeight(tt.cursor))

			state, err = manager.InitializeOrValidateDeployment("archive-wrapper:deployment", metadata)
			if tt.wantCorrupt {
				require.ErrorIs(t, err, apperrors.ErrDBCorrupt)
				require.Equal(t, DeploymentStateUnspecified, state)
				return
			}

			require.NoError(t, err)
			require.Equal(t, DeploymentStateInitialized, state)
		})
	}
}

func TestDbManagerInitializeOrValidateDeploymentRejectsMalformedState(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*DbManager)
	}{
		{
			name: "malformed metadata",
			setup: func(manager *DbManager) {
				require.NoError(t, manager.db.Put([]byte("archive-wrapper:deployment"), []byte("{"), nil))
			},
		},
		{
			name: "unknown metadata field",
			setup: func(manager *DbManager) {
				metadata := []byte(`{"schema_version":1,"mina_network_id":"testnet","contract_address":"B62qContract","start_height":10,"unknown":true}`)
				require.NoError(t, manager.db.Put([]byte("archive-wrapper:deployment"), metadata, nil))
			},
		},
		{
			name: "malformed cursor",
			setup: func(manager *DbManager) {
				writeDeploymentMetadata(t, manager)
				require.NoError(t, manager.db.Put([]byte(blockHeightDatabaseKey), []byte{1, 2, 3}, nil))
			},
		},
		{
			name: "zero cursor",
			setup: func(manager *DbManager) {
				writeDeploymentMetadata(t, manager)
				require.NoError(t, manager.db.Put([]byte(blockHeightDatabaseKey), encodeBlockHeight(0), nil))
			},
		},
		{
			name: "negative cursor",
			setup: func(manager *DbManager) {
				writeDeploymentMetadata(t, manager)
				require.NoError(t, manager.db.Put([]byte(blockHeightDatabaseKey), encodeBlockHeight(-1), nil))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
			require.NoError(t, err)
			defer func() { require.NoError(t, manager.Close()) }()
			tt.setup(manager)

			state, err := manager.InitializeOrValidateDeployment("archive-wrapper:deployment", testDeploymentMetadata())
			require.ErrorIs(t, err, apperrors.ErrDBCorrupt)
			require.Equal(t, DeploymentStateUnspecified, state)
		})
	}
}

func TestDbManagerInitializeOrValidateDeploymentRejectsInvalidExpectedMetadata(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*DeploymentMetadata)
		wantErr error
	}{
		{name: "schema version", mutate: func(m *DeploymentMetadata) { m.SchemaVersion = 0 }, wantErr: apperrors.ErrDeploymentSchemaVersionRequired},
		{name: "Mina network", mutate: func(m *DeploymentMetadata) { m.MinaNetworkID = "" }, wantErr: apperrors.ErrMinaNetworkIDRequired},
		{name: "contract address", mutate: func(m *DeploymentMetadata) { m.ContractAddress = "" }, wantErr: apperrors.ErrContractAddressRequired},
		{name: "start height", mutate: func(m *DeploymentMetadata) { m.StartHeight = 0 }, wantErr: apperrors.ErrStartBlockHeightRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
			require.NoError(t, err)
			defer func() { require.NoError(t, manager.Close()) }()
			metadata := testDeploymentMetadata()
			tt.mutate(&metadata)

			state, err := manager.InitializeOrValidateDeployment("archive-wrapper:deployment", metadata)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, DeploymentStateUnspecified, state)
		})
	}
}

func TestNewDbManagerClassifiesLockedDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := t.TempDir()
	owner, err := NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.NoError(t, err)
	defer func() { require.NoError(t, owner.Close()) }()

	contender, err := NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.Nil(t, contender)
	require.ErrorIs(t, err, apperrors.ErrDBLocked)
}

func TestWrapDatabaseErrorClassifiesCorruption(t *testing.T) {
	cause := leveldbErrors.NewErrCorrupted(storage.FileDesc{}, errors.New("bad table"))
	err := wrapDatabaseError("read table", cause)
	require.ErrorIs(t, err, apperrors.ErrDBCorrupt)
	require.ErrorIs(t, err, cause)
}

func TestNewDbManagerClassifiesCorruptedDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dbPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dbPath, "CURRENT"), []byte("MANIFEST-000001\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dbPath, "MANIFEST-000001"), []byte("garbage"), 0o600))

	manager, err := NewDbManager(dbPath, blockHeightDatabaseKey, logger)
	require.Nil(t, manager)
	require.ErrorIs(t, err, apperrors.ErrDBCorrupt)
}

func TestNewDbManagerDoesNotMisclassifyFilesystemError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	path := filepath.Join(t.TempDir(), "db-file")
	require.NoError(t, os.WriteFile(path, []byte("not a directory"), 0o600))

	manager, err := NewDbManager(path, blockHeightDatabaseKey, logger)
	require.Nil(t, manager)
	require.Error(t, err)
	require.NotErrorIs(t, err, apperrors.ErrDBLocked)
	require.NotErrorIs(t, err, apperrors.ErrDBCorrupt)
}

func TestDbManagerGetRejectsCorruptPersistedRecord(t *testing.T) {
	tests := []struct {
		name      string
		height    int64
		persisted []byte
	}{
		{name: "malformed protobuf", height: 10, persisted: []byte{0xff}},
		{
			name:   "record key mismatch",
			height: 10,
			persisted: marshalRecord(t, actions.DbRecord{
				Key: 11,
				Actions: []*actions.Action{{
					BlockHeight: 11,
					XCoordinate: []byte("alice-x"),
					IsOdd:       true,
					ActionType:  actions.ActionType_DEPOSIT,
					Amount:      1,
				}},
			}),
		},
		{
			name:   "invalid action",
			height: 10,
			persisted: marshalRecord(t, actions.DbRecord{
				Key: 10,
				Actions: []*actions.Action{{
					BlockHeight: 10,
					XCoordinate: []byte("alice-x"),
					IsOdd:       true,
					ActionType:  actions.ActionType_DEPOSIT,
					Amount:      0,
				}},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			manager, err := NewDbManager(t.TempDir(), blockHeightDatabaseKey, logger)
			require.NoError(t, err)
			defer func() { require.NoError(t, manager.Close()) }()
			require.NoError(t, manager.db.Put(encodeBlockHeight(tt.height), tt.persisted, nil))

			_, err = manager.Get(tt.height)
			require.ErrorIs(t, err, apperrors.ErrDBCorrupt)
		})
	}
}

func writeDeploymentMetadata(t *testing.T, manager *DbManager) {
	t.Helper()
	encoded, err := json.Marshal(testDeploymentMetadata())
	require.NoError(t, err)
	require.NoError(t, manager.db.Put([]byte("archive-wrapper:deployment"), encoded, nil))
}

func marshalRecord(t *testing.T, record actions.DbRecord) []byte {
	t.Helper()
	encoded, err := proto.Marshal(&record)
	require.NoError(t, err)
	return encoded
}

func testDeploymentMetadata() DeploymentMetadata {
	return DeploymentMetadata{
		SchemaVersion:   1,
		MinaNetworkID:   "testnet",
		ContractAddress: "B62qContract",
		StartHeight:     10,
	}
}
