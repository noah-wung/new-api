package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
)

func withExternalUsageRetentionDays(t *testing.T, days int) {
	t.Helper()

	raw := config.GlobalConfig.Get("external_usage_setting")
	cfg, ok := raw.(*external_usage_setting.ExternalUsageSetting)
	if !ok || cfg == nil {
		t.Fatal("external_usage_setting config is not registered")
	}
	previous := cfg.DetailRetentionDays
	cfg.DetailRetentionDays = days
	t.Cleanup(func() {
		cfg.DetailRetentionDays = previous
	})
}

func TestCleanupExpiredExternalUsageDetails_RemovesFactsButKeepsSummaries(t *testing.T) {
	setupExternalUsageTestTables(t)
	withExternalUsageRetentionDays(t, 180)

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC).Unix()
	oldOccurredAt := now - 181*24*3600
	newOccurredAt := now - 10*24*3600

	oldCursor := &model.ExternalUsageCursorEvent{
		UserID:              1,
		BatchID:             11,
		Source:              model.ExternalUsageSourceCursor,
		Status:              model.ExternalUsageStatusActive,
		EventIdentity:       "cursor-old",
		OccurredAt:          oldOccurredAt,
		SourceModelName:     "auto",
		NormalizedModelName: "cursor:auto",
		InputToken:          10,
		CacheReadToken:      5,
		OutputToken:         2,
		TotalToken:          17,
	}
	newCursor := &model.ExternalUsageCursorEvent{
		UserID:              1,
		BatchID:             11,
		Source:              model.ExternalUsageSourceCursor,
		Status:              model.ExternalUsageStatusActive,
		EventIdentity:       "cursor-new",
		OccurredAt:          newOccurredAt,
		SourceModelName:     "auto",
		NormalizedModelName: "cursor:auto",
		InputToken:          3,
		CacheReadToken:      1,
		OutputToken:         1,
		TotalToken:          5,
	}
	oldClient := &model.ExternalUsageClientEvent{
		UserID:              1,
		DeviceID:            2,
		BatchID:             21,
		Source:              model.ExternalUsageSourceCodex,
		Status:              model.ExternalUsageStatusAccepted,
		EventIdentity:       "client-old",
		OccurredAt:          oldOccurredAt,
		SourceModelName:     "gpt-5-codex",
		NormalizedModelName: "codex:gpt-5-codex",
		InputToken:          6,
		CachedInputToken:    2,
		OutputToken:         1,
		TotalToken:          9,
	}
	newClient := &model.ExternalUsageClientEvent{
		UserID:              1,
		DeviceID:            2,
		BatchID:             21,
		Source:              model.ExternalUsageSourceCodex,
		Status:              model.ExternalUsageStatusAccepted,
		EventIdentity:       "client-new",
		OccurredAt:          newOccurredAt,
		SourceModelName:     "gpt-5-codex",
		NormalizedModelName: "codex:gpt-5-codex",
		InputToken:          2,
		CachedInputToken:    1,
		OutputToken:         1,
		TotalToken:          4,
	}

	for _, row := range []any{
		&model.ExternalUsageCursorImportBatch{UserID: 1, ImportedBy: 100, FileName: "batch.csv", FileSHA256: "batch-hash", Status: model.ExternalUsageStatusActive},
		&model.ExternalUsageReportBatch{UserID: 1, DeviceID: 2, Source: model.ExternalUsageSourceCodex, Status: model.ExternalUsageStatusAccepted},
		&model.ExternalUsageAggregate{UserID: 1, Origin: model.ExternalUsageOriginCursorImport, Source: model.ExternalUsageSourceCursor, NormalizedModelName: "cursor:auto", BucketAt: oldOccurredAt - (oldOccurredAt % 3600), EventCount: 1, TotalTokens: 17},
		oldCursor,
		newCursor,
		oldClient,
		newClient,
	} {
		if err := model.DB.Create(row).Error; err != nil {
			t.Fatalf("Create(%T) error: %v", row, err)
		}
	}

	result, err := CleanupExpiredExternalUsageDetails(now)
	if err != nil {
		t.Fatalf("CleanupExpiredExternalUsageDetails error: %v", err)
	}
	if result.CursorEventsDeleted != 1 {
		t.Fatalf("result.CursorEventsDeleted = %d, want 1", result.CursorEventsDeleted)
	}
	if result.ClientEventsDeleted != 1 {
		t.Fatalf("result.ClientEventsDeleted = %d, want 1", result.ClientEventsDeleted)
	}

	var cursorCount int64
	if err := model.DB.Model(&model.ExternalUsageCursorEvent{}).Count(&cursorCount).Error; err != nil {
		t.Fatalf("Count(cursor events) error: %v", err)
	}
	if cursorCount != 1 {
		t.Fatalf("cursorCount = %d, want 1", cursorCount)
	}

	var clientCount int64
	if err := model.DB.Model(&model.ExternalUsageClientEvent{}).Count(&clientCount).Error; err != nil {
		t.Fatalf("Count(client events) error: %v", err)
	}
	if clientCount != 1 {
		t.Fatalf("clientCount = %d, want 1", clientCount)
	}

	var aggregateCount int64
	if err := model.DB.Model(&model.ExternalUsageAggregate{}).Count(&aggregateCount).Error; err != nil {
		t.Fatalf("Count(aggregates) error: %v", err)
	}
	if aggregateCount != 1 {
		t.Fatalf("aggregateCount = %d, want 1", aggregateCount)
	}

	var cursorBatchCount int64
	if err := model.DB.Model(&model.ExternalUsageCursorImportBatch{}).Count(&cursorBatchCount).Error; err != nil {
		t.Fatalf("Count(cursor batches) error: %v", err)
	}
	if cursorBatchCount != 1 {
		t.Fatalf("cursorBatchCount = %d, want 1", cursorBatchCount)
	}

	var reportBatchCount int64
	if err := model.DB.Model(&model.ExternalUsageReportBatch{}).Count(&reportBatchCount).Error; err != nil {
		t.Fatalf("Count(report batches) error: %v", err)
	}
	if reportBatchCount != 1 {
		t.Fatalf("reportBatchCount = %d, want 1", reportBatchCount)
	}
}
