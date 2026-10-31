package model

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestZCodeBackfillRefusesDuplicatesAndPreservesFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "audit.sqlite")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ExternalUsageClientEvent{}, &ZCodeUsageLock{}, &ZCodeUsageIdentity{}))
	event := ExternalUsageClientEvent{UserID: 42, Source: ExternalUsageSourceZCode, EventIdentity: "id", OccurredAt: 1, SourceSessionHash: "session", SourceModelName: "model", InputToken: 2, OutputToken: 1, TotalToken: 3}
	require.NoError(t, db.Create(&event).Error)
	copy := event
	copy.Id = 0
	require.NoError(t, db.Create(&copy).Error)
	duplicates, err := AuditZCodeDuplicateIdentities(db)
	require.NoError(t, err)
	require.Len(t, duplicates, 1)
	require.Error(t, db.Transaction(BackfillZCodeIdentitiesTx))
	var count int64
	require.NoError(t, db.Model(&ZCodeUsageIdentity{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Delete(&copy).Error)
	require.NoError(t, db.Transaction(BackfillZCodeIdentitiesTx))
	require.NoError(t, db.Transaction(BackfillZCodeIdentitiesTx))
	require.NoError(t, db.Model(&ZCodeUsageIdentity{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&ExternalUsageClientEvent{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
