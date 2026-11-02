package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSystemBuiltinCatalogDeleteCannotBeResurrectedBySeedOrStaleUpdate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "catalog.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.SystemBuiltinAgent{}))
	r := &customAgentRepository{db: db}
	ctx := context.Background()
	seed := &types.SystemBuiltinAgent{ID: types.BuiltinSmartReasoningID, Name: "Original"}
	require.NoError(t, r.SeedSystemBuiltinAgents(ctx, []*types.SystemBuiltinAgent{seed}))
	stale, err := r.GetSystemBuiltinAgent(ctx, seed.ID)
	require.NoError(t, err)
	require.NoError(t, r.DeleteSystemBuiltinAgent(ctx, seed.ID))
	require.NoError(t, r.SeedSystemBuiltinAgents(ctx, []*types.SystemBuiltinAgent{{ID: seed.ID, Name: "Restart"}}))
	stale.Name = "Late update"
	require.ErrorIs(t, r.UpdateSystemBuiltinAgent(ctx, stale), ErrCustomAgentNotFound)
	_, err = r.GetSystemBuiltinAgent(ctx, seed.ID)
	require.ErrorIs(t, err, ErrCustomAgentNotFound)
	rows, err := r.ListSystemBuiltinAgents(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
}
