package service

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReasoningOutputAliases(t *testing.T) {
	for _, tc := range []struct {
		body     string
		value    int
		conflict bool
	}{
		{`{"reasoning_output_tokens":7}`, 7, false},
		{`{"reasoning_output_token":7}`, 7, false},
		{`{"reasoning_output_token":0,"reasoning_output_tokens":7}`, 0, true},
		{`{"reasoning_output_token":0,"reasoning_output_tokens":0}`, 0, false},
	} {
		var event ClientUsageEventInput
		require.NoError(t, common.Unmarshal([]byte(tc.body), &event))
		require.Equal(t, tc.value, event.ReasoningOutputToken)
		require.Equal(t, tc.conflict, event.reasoningConflict)
	}
}

func TestZCodeConcurrentReportsRemainIdempotentSQLite(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "concurrency.sqlite")+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	setupExternalUsageTestTables(t)
	cfg := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled, cfg.AcceptWindowDays = true, 90
	cfg.AllowedSources = []string{model.ExternalUsageSourceZCode}
	device, _, err := IssueExternalUsageDeviceCredential(IssueExternalUsageDeviceParams{UserID: 988, DeviceName: "fixture", DeviceFingerprint: "concurrent-zcode", AllowedSources: []string{model.ExternalUsageSourceZCode}})
	require.NoError(t, err)
	event := ClientUsageEventInput{EventID: "concurrent", SessionID: "session", OccurredAt: time.Now().Unix(), SourceModelName: "model", InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
	params := ReportExternalUsageParams{Source: model.ExternalUsageSourceZCode, Events: []ClientUsageEventInput{event}}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copyDevice := *device
			_, err := ReportExternalUsage(&copyDevice, params)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			// SQLite can reject a concurrent writer as busy; an explicit later
			// retry must still resolve to the same persisted identity.
			_, retryErr := ReportExternalUsage(device, params)
			require.NoError(t, retryErr)
		}
	}
	var count int64
	require.NoError(t, db.Model(&model.ExternalUsageClientEvent{}).Where("user_id = ?", device.UserID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	summary, err := model.ListExternalUsageSummaryByUser(device.UserID, 0, 0)
	require.NoError(t, err)
	require.Len(t, summary, 1)
	require.EqualValues(t, 1, summary[0].EventCount)
	require.EqualValues(t, 15, summary[0].TotalTokens)
}

func TestZCodeDistinctIdentitiesAndConflict(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "usage.sqlite")), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	setupExternalUsageTestTables(t)
	cfg := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled = true
	cfg.AcceptWindowDays = 90
	cfg.AllowedSources = []string{model.ExternalUsageSourceZCode}
	device, _, err := IssueExternalUsageDeviceCredential(IssueExternalUsageDeviceParams{UserID: 987, DeviceName: "fixture", DeviceFingerprint: "fixture-zcode", AllowedSources: []string{model.ExternalUsageSourceZCode}})
	require.NoError(t, err)
	first := ClientUsageEventInput{EventID: "first", SessionID: "session", OccurredAt: time.Now().Unix(), SourceModelName: "fixture-model", InputTokens: 10, OutputTokens: 5, ReasoningOutputToken: 2, TotalTokens: 15}
	second := first
	second.EventID = "second"
	result, err := ReportExternalUsage(device, ReportExternalUsageParams{Source: model.ExternalUsageSourceZCode, Events: []ClientUsageEventInput{first, second}})
	require.NoError(t, err)
	require.Len(t, result.AcceptedEventIDs, 2)
	first.TotalTokens++
	result, err = ReportExternalUsage(device, ReportExternalUsageParams{Source: model.ExternalUsageSourceZCode, Events: []ClientUsageEventInput{first}})
	require.NoError(t, err)
	require.Len(t, result.RejectedEventInfos, 1)
	summary, err := model.ListExternalUsageSummaryByUser(device.UserID, 0, 0)
	require.NoError(t, err)
	require.Len(t, summary, 1)
	require.EqualValues(t, 2, summary[0].EventCount)
	require.EqualValues(t, 4, summary[0].ReasoningOutputToken)
	_, err = model.DeleteExternalUsageModelData(device.UserID, model.ExternalUsageSourceZCode, summary[0].NormalizedModelName)
	require.NoError(t, err)
	secondResult, err := ReportExternalUsage(device, ReportExternalUsageParams{Source: model.ExternalUsageSourceZCode, Events: []ClientUsageEventInput{second}})
	require.NoError(t, err)
	require.Len(t, secondResult.DuplicateEventIDs, 1)
	summary, err = model.ListExternalUsageSummaryByUser(device.UserID, 0, 0)
	require.NoError(t, err)
	require.Empty(t, summary)
}
