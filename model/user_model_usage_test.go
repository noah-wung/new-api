package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserModelUsageTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL

	db, err := gorm.Open(sqlite.Open("file:user_model_usage?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	t.Cleanup(func() {
		DB = previousDB
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.AutoMigrate(&User{}, &QuotaData{}, &ExternalUsageAggregate{}))
	return db
}

func TestGetUserModelUsageRows(t *testing.T) {
	db := setupUserModelUsageTestDB(t)

	user := User{Username: "usage-target", Password: "password"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&[]QuotaData{
		{UserID: user.Id, Username: user.Username, ModelName: "gpt-5", CreatedAt: 1717200000, TokenUsed: 100, Quota: 20, Count: 2},
		{UserID: user.Id, Username: user.Username, ModelName: "gpt-5", CreatedAt: 1717203600, TokenUsed: 200, Quota: 100, Count: 3},
		{UserID: user.Id, Username: user.Username, ModelName: "GPT-5", CreatedAt: 1717207200, TokenUsed: 10, Quota: 5, Count: 1},
		{UserID: user.Id, Username: user.Username, ModelName: "outside", CreatedAt: 1717189999, TokenUsed: 999, Quota: 999, Count: 9},
	}).Error)
	require.NoError(t, db.Create(&[]ExternalUsageAggregate{
		{UserID: user.Id, Origin: ExternalUsageOriginClientReport, Source: "codex", NormalizedModelName: "gpt-5", BucketAt: 1717200000, TotalTokens: 150, EventCount: 1},
		{UserID: user.Id, Origin: ExternalUsageOriginClientReport, Source: "codex", NormalizedModelName: "GPT-5", BucketAt: 1717203600, TotalTokens: 250, EventCount: 3},
		{UserID: user.Id, Origin: ExternalUsageOriginCursorImport, Source: "cursor", NormalizedModelName: "cursor:auto", BucketAt: 1717207200, TotalTokens: 50, EventCount: 2},
		{UserID: user.Id, Origin: ExternalUsageOriginCursorImport, Source: "cursor", NormalizedModelName: "outside", BucketAt: 1717210001, TotalTokens: 999, EventCount: 9},
	}).Error)
	require.NoError(t, db.Delete(&user).Error)

	target, err := GetUserModelUsageTarget(user.Id)
	require.NoError(t, err)
	require.True(t, target.DeletedAt.Valid)

	gateway, err := GetGatewayUserModelUsageRows(user.Id, 1717190000, 1717210000)
	require.NoError(t, err)
	require.ElementsMatch(t, []GatewayUserModelUsageRow{
		{ModelName: "gpt-5", TokenUsage: 310, Quota: 125, GatewayRequests: 6},
	}, gateway)

	external, err := GetExternalUserModelUsageRows(user.Id, 1717190000, 1717210000)
	require.NoError(t, err)
	require.ElementsMatch(t, []ExternalUserModelUsageRow{
		{Source: "codex", ModelName: "gpt-5", TokenUsage: 400, ExternalEvents: 4},
		{Source: "cursor", ModelName: "cursor:auto", TokenUsage: 50, ExternalEvents: 2},
	}, external)
}

func TestGetUserModelUsageRowsDeterministicAndNonNil(t *testing.T) {
	db := setupUserModelUsageTestDB(t)

	require.NoError(t, db.Create(&[]QuotaData{
		{UserID: 42, ModelName: "zeta", CreatedAt: 1717200000, TokenUsed: 2, Quota: 2, Count: 2},
		{UserID: 42, ModelName: "Alpha", CreatedAt: 1717200000, TokenUsed: 1, Quota: 1, Count: 1},
	}).Error)
	require.NoError(t, db.Create(&[]ExternalUsageAggregate{
		{UserID: 42, Origin: ExternalUsageOriginClientReport, Source: "zeta-source", NormalizedModelName: "zeta", BucketAt: 1717200000, TotalTokens: 2, EventCount: 2},
		{UserID: 42, Origin: ExternalUsageOriginClientReport, Source: "alpha-source", NormalizedModelName: "Alpha", BucketAt: 1717200000, TotalTokens: 1, EventCount: 1},
	}).Error)

	gateway, err := GetGatewayUserModelUsageRows(42, 1717190000, 1717210000)
	require.NoError(t, err)
	require.Equal(t, []GatewayUserModelUsageRow{
		{ModelName: "alpha", TokenUsage: 1, Quota: 1, GatewayRequests: 1},
		{ModelName: "zeta", TokenUsage: 2, Quota: 2, GatewayRequests: 2},
	}, gateway)

	external, err := GetExternalUserModelUsageRows(42, 1717190000, 1717210000)
	require.NoError(t, err)
	require.Equal(t, []ExternalUserModelUsageRow{
		{Source: "alpha-source", ModelName: "alpha", TokenUsage: 1, ExternalEvents: 1},
		{Source: "zeta-source", ModelName: "zeta", TokenUsage: 2, ExternalEvents: 2},
	}, external)

	emptyGateway, err := GetGatewayUserModelUsageRows(99, 1717190000, 1717210000)
	require.NoError(t, err)
	require.NotNil(t, emptyGateway)
	require.Empty(t, emptyGateway)

	emptyExternal, err := GetExternalUserModelUsageRows(99, 1717190000, 1717210000)
	require.NoError(t, err)
	require.NotNil(t, emptyExternal)
	require.Empty(t, emptyExternal)
}

func TestGetUserModelUsageRowsUsesStableUserIDWhenMergingSources(t *testing.T) {
	db := setupUserModelUsageTestDB(t)

	user := User{Username: "current-name", Password: "password"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&QuotaData{
		UserID: user.Id, Username: "historical-name", ModelName: "gpt-5",
		CreatedAt: 1717200000, TokenUsed: 100, Quota: 20, Count: 2,
	}).Error)
	require.NoError(t, db.Create(&ExternalUsageAggregate{
		UserID: user.Id, Origin: ExternalUsageOriginClientReport, Source: "codex",
		NormalizedModelName: "gpt-5", BucketAt: 1717200000, TotalTokens: 150, EventCount: 1,
	}).Error)

	rows, err := GetQuotaDataGroupByUser(1717190000, 1717210000)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, user.Id, rows[0].UserID)
	require.Equal(t, "historical-name", rows[0].Username)
	require.Equal(t, int64(1717200000), rows[0].CreatedAt)
	require.Equal(t, 250, rows[0].TokenUsed)
	require.Equal(t, 20, rows[0].Quota)
	require.Equal(t, 3, rows[0].Count)
}

func TestUserModelUsageCompositeIndexes(t *testing.T) {
	db := setupUserModelUsageTestDB(t)

	require.True(t, db.Migrator().HasIndex(&QuotaData{}, "idx_qdt_user_created_model"))
	require.True(t, db.Migrator().HasIndex(&ExternalUsageAggregate{}, "idx_eua_user_bucket_model_source"))
}
