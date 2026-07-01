package service

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
)

func setupExternalUsageTestTables(t *testing.T) {
	t.Helper()
	if err := model.DB.AutoMigrate(
		&model.ExternalUsageDevice{},
		&model.ExternalUsageInstallToken{},
		&model.ExternalUsageCursorImportBatch{},
		&model.ExternalUsageCursorEvent{},
		&model.ExternalUsageReportBatch{},
		&model.ExternalUsageClientEvent{},
		&model.ExternalUsageAggregate{},
		&model.ExternalUsageModelMapping{},
	); err != nil {
		t.Fatalf("AutoMigrate external usage tables failed: %v", err)
	}
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM external_usage_model_mappings")
		model.DB.Exec("DELETE FROM external_usage_install_tokens")
		model.DB.Exec("DELETE FROM external_usage_report_batches")
		model.DB.Exec("DELETE FROM external_usage_cursor_events")
		model.DB.Exec("DELETE FROM external_usage_client_events")
		model.DB.Exec("DELETE FROM external_usage_aggregates")
		model.DB.Exec("DELETE FROM external_usage_cursor_import_batches")
		model.DB.Exec("DELETE FROM external_usage_devices")
	})
}

func TestParseCursorCSV(t *testing.T) {
	csvContent := strings.Join([]string{
		"Date,Cloud Agent ID,Automation ID,Kind,Model,Max Mode,Input (w/ Cache Write),Input (w/o Cache Write),Cache Read,Output Tokens,Total Tokens,Cost",
		"\"2026-06-25T11:13:12.544Z\",\"agent-1\",\"auto-1\",\"Included\",\"auto\",\"No\",\"0\",\"1983\",\"128736\",\"750\",\"131469\",\"Included\"",
	}, "\n")

	rows, err := parseCursorCSV([]byte(csvContent), 90)
	if err != nil {
		t.Fatalf("parseCursorCSV returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.TotalToken != 131469 {
		t.Fatalf("row.TotalToken = %d, want 131469", row.TotalToken)
	}
	if row.InputToken != 1983 {
		t.Fatalf("row.InputToken = %d, want 1983", row.InputToken)
	}
	if row.CacheReadToken != 128736 {
		t.Fatalf("row.CacheReadToken = %d, want 128736", row.CacheReadToken)
	}
	if row.NormalizedModelName != "cursor:auto" {
		t.Fatalf("row.NormalizedModelName = %q, want %q", row.NormalizedModelName, "cursor:auto")
	}
	if row.EventIdentity == "" {
		t.Fatal("row.EventIdentity should not be empty")
	}
}

func TestValidateClientUsageEvent(t *testing.T) {
	now := time.Now().Unix()
	valid := validateClientUsageEvent(ClientUsageEventInput{
		EventID:         "event-1",
		OccurredAt:      now,
		SourceModelName: "gpt-5-codex",
		TotalTokens:     12,
	}, 90)
	if valid != "" {
		t.Fatalf("validateClientUsageEvent(valid) = %q, want empty", valid)
	}

	oldEvent := validateClientUsageEvent(ClientUsageEventInput{
		EventID:     "event-2",
		OccurredAt:  now - 91*24*3600,
		TotalTokens: 12,
	}, 90)
	if oldEvent == "" {
		t.Fatal("expected old event to be rejected")
	}

	zeroTokens := validateClientUsageEvent(ClientUsageEventInput{
		EventID:     "event-3",
		OccurredAt:  now,
		TotalTokens: 0,
	}, 90)
	if zeroTokens == "" {
		t.Fatal("expected zero-token event to be rejected")
	}
}

func TestValidateClientUsageEvent_RejectsNegativeTokenCounters(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name  string
		event ClientUsageEventInput
	}{
		{
			name: "negative input",
			event: ClientUsageEventInput{
				EventID:     "neg-input",
				OccurredAt:  now,
				InputTokens: -1,
				TotalTokens: 12,
			},
		},
		{
			name: "negative cache",
			event: ClientUsageEventInput{
				EventID:           "neg-cache",
				OccurredAt:        now,
				CachedInputTokens: -2,
				TotalTokens:       12,
			},
		},
		{
			name: "negative output",
			event: ClientUsageEventInput{
				EventID:      "neg-output",
				OccurredAt:   now,
				OutputTokens: -3,
				TotalTokens:  12,
			},
		},
		{
			name: "negative reasoning",
			event: ClientUsageEventInput{
				EventID:              "neg-reasoning",
				OccurredAt:           now,
				ReasoningOutputToken: -4,
				TotalTokens:          12,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateClientUsageEvent(tc.event, 90); got == "" {
				t.Fatalf("validateClientUsageEvent(%s) returned empty rejection", tc.name)
			}
		})
	}
}

func TestIssueCodexInstallToken_SetsScopeAndExpiry(t *testing.T) {
	setupExternalUsageTestTables(t)

	issued, rawToken, err := IssueCodexInstallToken(7)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken returned error: %v", err)
	}
	if strings.TrimSpace(rawToken) == "" {
		t.Fatal("IssueCodexInstallToken returned empty raw token")
	}
	if issued.UserID != 7 {
		t.Fatalf("issued.UserID = %d, want 7", issued.UserID)
	}
	if issued.Source != model.ExternalUsageSourceCodex {
		t.Fatalf("issued.Source = %q, want %q", issued.Source, model.ExternalUsageSourceCodex)
	}
	if issued.ExpiresAt <= issued.CreatedAt {
		t.Fatalf("issued.ExpiresAt = %d, issued.CreatedAt = %d; want expiry after creation", issued.ExpiresAt, issued.CreatedAt)
	}
	if issued.ConsumedAt != 0 {
		t.Fatalf("issued.ConsumedAt = %d, want 0", issued.ConsumedAt)
	}
}

func TestConsumeCodexInstallToken_RejectsReusedToken(t *testing.T) {
	setupExternalUsageTestTables(t)

	_, rawToken, err := IssueCodexInstallToken(11)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken returned error: %v", err)
	}

	first, err := ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{
		InstallToken:      rawToken,
		DeviceName:        "Noah MacBook",
		DeviceFingerprint: "fingerprint-reuse-check",
	})
	if err != nil {
		t.Fatalf("first ConsumeCodexInstallToken returned error: %v", err)
	}
	if first.Device == nil {
		t.Fatal("first ConsumeCodexInstallToken returned nil device")
	}
	if strings.TrimSpace(first.ReportingCredential) == "" {
		t.Fatal("first ConsumeCodexInstallToken returned empty reporting credential")
	}

	_, err = ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{
		InstallToken:      rawToken,
		DeviceName:        "Noah MacBook",
		DeviceFingerprint: "fingerprint-reuse-check",
	})
	if err == nil {
		t.Fatal("expected reused install token to be rejected")
	}
}

func TestConsumeCodexInstallToken_ReusesExistingDeviceFingerprint(t *testing.T) {
	setupExternalUsageTestTables(t)

	const fingerprint = "stable-fingerprint-1"

	_, firstToken, err := IssueCodexInstallToken(23)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken(first) returned error: %v", err)
	}
	firstExchange, err := ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{
		InstallToken:      firstToken,
		DeviceName:        "Codex Workstation",
		DeviceFingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("ConsumeCodexInstallToken(first) returned error: %v", err)
	}

	_, secondToken, err := IssueCodexInstallToken(23)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken(second) returned error: %v", err)
	}
	secondExchange, err := ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{
		InstallToken:      secondToken,
		DeviceName:        "Codex Workstation (Reinstall)",
		DeviceFingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("ConsumeCodexInstallToken(second) returned error: %v", err)
	}

	if firstExchange.Device == nil || secondExchange.Device == nil {
		t.Fatal("ConsumeCodexInstallToken should return devices for both exchanges")
	}
	if secondExchange.Device.Id != firstExchange.Device.Id {
		t.Fatalf("reinstall created new device id %d, want reuse of %d", secondExchange.Device.Id, firstExchange.Device.Id)
	}

	var deviceCount int64
	if err := model.DB.Model(&model.ExternalUsageDevice{}).Where("user_id = ?", 23).Count(&deviceCount).Error; err != nil {
		t.Fatalf("Count(devices) error: %v", err)
	}
	if deviceCount != 1 {
		t.Fatalf("deviceCount = %d, want 1", deviceCount)
	}
}

func TestReportExternalUsage_RejectsGloballyDisabledSource(t *testing.T) {
	setupExternalUsageTestTables(t)

	device, _, err := IssueExternalUsageDeviceCredential(IssueExternalUsageDeviceParams{
		UserID:            31,
		DeviceName:        "Codex Device",
		DeviceFingerprint: "codex-disabled-check",
		AllowedSources:    []string{model.ExternalUsageSourceCodex},
	})
	if err != nil {
		t.Fatalf("IssueExternalUsageDeviceCredential returned error: %v", err)
	}

	raw := config.GlobalConfig.Get("external_usage_setting")
	cfg, ok := raw.(*external_usage_setting.ExternalUsageSetting)
	if !ok || cfg == nil {
		t.Fatal("external_usage_setting config is not registered")
	}
	previous := *cfg
	cfg.Enabled = true
	cfg.AllowedSources = []string{model.ExternalUsageSourceCursor}
	t.Cleanup(func() {
		*cfg = previous
	})

	_, err = ReportExternalUsage(device, ReportExternalUsageParams{
		Source: model.ExternalUsageSourceCodex,
		Events: []ClientUsageEventInput{
			{
				EventID:         "codex-disabled-event",
				SessionID:       "session-disabled",
				OccurredAt:      time.Now().Unix(),
				SourceModelName: "gpt-5-codex",
				InputTokens:     1,
				OutputTokens:    1,
				TotalTokens:     2,
			},
		},
	})
	if err == nil {
		t.Fatal("expected globally disabled codex source to be rejected")
	}
	if !strings.Contains(err.Error(), "source is not enabled") {
		t.Fatalf("err = %v, want source disabled error", err)
	}
}

func TestReportExternalUsage_DuplicateEventAfterReinstallDoesNotIncreaseCount(t *testing.T) {
	setupExternalUsageTestTables(t)

	const (
		userID      = 31
		fingerprint = "stable-fingerprint-2"
		eventID     = "codex-event-1"
	)

	_, firstToken, err := IssueCodexInstallToken(userID)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken(first) returned error: %v", err)
	}
	firstExchange, err := ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{
		InstallToken:      firstToken,
		DeviceName:        "Dev Machine",
		DeviceFingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("ConsumeCodexInstallToken(first) returned error: %v", err)
	}

	firstDevice, err := LookupExternalUsageDeviceByCredential(firstExchange.ReportingCredential)
	if err != nil {
		t.Fatalf("LookupExternalUsageDeviceByCredential(first) returned error: %v", err)
	}
	occurredAt := time.Now().Unix()
	firstReport, err := ReportExternalUsage(firstDevice, ReportExternalUsageParams{
		Source:        model.ExternalUsageSourceCodex,
		ClientVersion: "1.0.0",
		Events: []ClientUsageEventInput{
			{
				EventID:         eventID,
				SessionID:       "session-1",
				OccurredAt:      occurredAt,
				SourceModelName: "gpt-5-codex",
				InputTokens:     10,
				OutputTokens:    5,
				TotalTokens:     15,
			},
		},
	})
	if err != nil {
		t.Fatalf("ReportExternalUsage(first) returned error: %v", err)
	}
	if firstReport.Batch.AcceptedCount != 1 {
		t.Fatalf("firstReport.Batch.AcceptedCount = %d, want 1", firstReport.Batch.AcceptedCount)
	}

	_, reinstallToken, err := IssueCodexInstallToken(userID)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken(reinstall) returned error: %v", err)
	}
	reinstallExchange, err := ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{
		InstallToken:      reinstallToken,
		DeviceName:        "Dev Machine Reinstall",
		DeviceFingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("ConsumeCodexInstallToken(reinstall) returned error: %v", err)
	}
	reinstallDevice, err := LookupExternalUsageDeviceByCredential(reinstallExchange.ReportingCredential)
	if err != nil {
		t.Fatalf("LookupExternalUsageDeviceByCredential(reinstall) returned error: %v", err)
	}

	secondReport, err := ReportExternalUsage(reinstallDevice, ReportExternalUsageParams{
		Source:        model.ExternalUsageSourceCodex,
		ClientVersion: "1.0.1",
		Events: []ClientUsageEventInput{
			{
				EventID:         eventID,
				SessionID:       "session-1",
				OccurredAt:      occurredAt,
				SourceModelName: "gpt-5-codex",
				InputTokens:     10,
				OutputTokens:    5,
				TotalTokens:     15,
			},
		},
	})
	if err != nil {
		t.Fatalf("ReportExternalUsage(second) returned error: %v", err)
	}
	if secondReport.Batch.AcceptedCount != 0 {
		t.Fatalf("secondReport.Batch.AcceptedCount = %d, want 0", secondReport.Batch.AcceptedCount)
	}
	if secondReport.Batch.DuplicateCount != 1 {
		t.Fatalf("secondReport.Batch.DuplicateCount = %d, want 1", secondReport.Batch.DuplicateCount)
	}

	summary, err := model.ListExternalUsageSummaryByUser(userID, 0, 0)
	if err != nil {
		t.Fatalf("ListExternalUsageSummaryByUser returned error: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("len(summary) = %d, want 1", len(summary))
	}
	if summary[0].EventCount != 1 {
		t.Fatalf("summary[0].EventCount = %d, want 1", summary[0].EventCount)
	}
	if summary[0].TotalTokens != 15 {
		t.Fatalf("summary[0].TotalTokens = %d, want 15", summary[0].TotalTokens)
	}
}

func TestReportExternalUsage_PreservesReporterSessionHash(t *testing.T) {
	setupExternalUsageTestTables(t)

	device, _, err := IssueExternalUsageDeviceCredential(IssueExternalUsageDeviceParams{
		UserID:            52,
		DeviceName:        "Codex Session Hash Device",
		DeviceFingerprint: "codex-session-hash-fingerprint",
		AllowedSources:    []string{model.ExternalUsageSourceCodex},
	})
	if err != nil {
		t.Fatalf("IssueExternalUsageDeviceCredential returned error: %v", err)
	}

	sessionHash := sha256Hex("raw-session-id-1")
	occurredAt := time.Now().Unix()
	report, err := ReportExternalUsage(device, ReportExternalUsageParams{
		Source:        model.ExternalUsageSourceCodex,
		ClientVersion: "1.0.0",
		Events: []ClientUsageEventInput{
			{
				EventID:         "codex-session-hash-event-1",
				SessionID:       sessionHash,
				OccurredAt:      occurredAt,
				SourceModelName: "gpt-5.4",
				InputTokens:     10,
				OutputTokens:    5,
				TotalTokens:     15,
			},
		},
	})
	if err != nil {
		t.Fatalf("ReportExternalUsage returned error: %v", err)
	}
	if report.Batch.AcceptedCount != 1 {
		t.Fatalf("report.Batch.AcceptedCount = %d, want 1", report.Batch.AcceptedCount)
	}

	var stored model.ExternalUsageClientEvent
	if err := model.DB.Where("event_identity = ?", "codex-session-hash-event-1").First(&stored).Error; err != nil {
		t.Fatalf("load stored event error: %v", err)
	}
	if stored.SourceSessionHash != sessionHash {
		t.Fatalf("stored.SourceSessionHash = %q, want %q", stored.SourceSessionHash, sessionHash)
	}
}

func TestReportExternalUsage_DuplicateEquivalentEventRepairsUnknownModel(t *testing.T) {
	setupExternalUsageTestTables(t)

	device, _, err := IssueExternalUsageDeviceCredential(IssueExternalUsageDeviceParams{
		UserID:            77,
		DeviceName:        "Codex Repair Device",
		DeviceFingerprint: "codex-repair-fingerprint",
		AllowedSources:    []string{model.ExternalUsageSourceCodex},
	})
	if err != nil {
		t.Fatalf("IssueExternalUsageDeviceCredential returned error: %v", err)
	}

	occurredAt := time.Now().Unix()
	firstReport, err := ReportExternalUsage(device, ReportExternalUsageParams{
		Source:        model.ExternalUsageSourceCodex,
		ClientVersion: "1.0.0",
		Events: []ClientUsageEventInput{
			{
				EventID:           "legacy-unknown-event-id",
				SessionID:         "repair-session-1",
				OccurredAt:        occurredAt,
				SourceModelName:   "unknown",
				InputTokens:       120,
				CachedInputTokens: 100,
				OutputTokens:      20,
				TotalTokens:       140,
			},
		},
	})
	if err != nil {
		t.Fatalf("ReportExternalUsage(first) returned error: %v", err)
	}
	if firstReport.Batch.AcceptedCount != 1 {
		t.Fatalf("firstReport.Batch.AcceptedCount = %d, want 1", firstReport.Batch.AcceptedCount)
	}

	secondReport, err := ReportExternalUsage(device, ReportExternalUsageParams{
		Source:        model.ExternalUsageSourceCodex,
		ClientVersion: "1.0.1",
		Events: []ClientUsageEventInput{
			{
				EventID:           "turn-aware-event-id",
				SessionID:         "repair-session-1",
				OccurredAt:        occurredAt,
				SourceModelName:   "codex-auto-review",
				InputTokens:       120,
				CachedInputTokens: 100,
				OutputTokens:      20,
				TotalTokens:       140,
			},
		},
	})
	if err != nil {
		t.Fatalf("ReportExternalUsage(second) returned error: %v", err)
	}
	if secondReport.Batch.AcceptedCount != 0 {
		t.Fatalf("secondReport.Batch.AcceptedCount = %d, want 0", secondReport.Batch.AcceptedCount)
	}
	if secondReport.Batch.DuplicateCount != 1 {
		t.Fatalf("secondReport.Batch.DuplicateCount = %d, want 1", secondReport.Batch.DuplicateCount)
	}

	stored, err := model.FindEquivalentExternalUsageClientEventTx(
		model.DB,
		77,
		model.ExternalUsageSourceCodex,
		"repair-session-1",
		occurredAt,
		120,
		100,
		20,
		0,
		140,
	)
	if err != nil {
		t.Fatalf("FindEquivalentExternalUsageClientEventTx returned error: %v", err)
	}
	if stored.SourceModelName != "codex-auto-review" {
		t.Fatalf("stored.SourceModelName = %q, want %q", stored.SourceModelName, "codex-auto-review")
	}
	if stored.NormalizedModelName != "codex:codex-auto-review" {
		t.Fatalf("stored.NormalizedModelName = %q, want %q", stored.NormalizedModelName, "codex:codex-auto-review")
	}

	summary, err := model.ListExternalUsageSummaryByUser(77, 0, 0)
	if err != nil {
		t.Fatalf("ListExternalUsageSummaryByUser returned error: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("len(summary) = %d, want 1", len(summary))
	}
	if summary[0].NormalizedModelName != "codex:codex-auto-review" {
		t.Fatalf("summary[0].NormalizedModelName = %q, want %q", summary[0].NormalizedModelName, "codex:codex-auto-review")
	}
	if summary[0].EventCount != 1 {
		t.Fatalf("summary[0].EventCount = %d, want 1", summary[0].EventCount)
	}
	if summary[0].TotalTokens != 140 {
		t.Fatalf("summary[0].TotalTokens = %d, want 140", summary[0].TotalTokens)
	}
}

func TestImportCursorUsageCSV_TracksRejectedMalformedRows(t *testing.T) {
	setupExternalUsageTestTables(t)

	csvContent := strings.Join([]string{
		"Date,Cloud Agent ID,Automation ID,Kind,Model,Max Mode,Input (w/ Cache Write),Input (w/o Cache Write),Cache Read,Output Tokens,Total Tokens,Cost",
		"\"2026-06-25T11:13:12.544Z\",\"agent-1\",\"auto-1\",\"Included\",\"auto\",\"No\",\"0\",\"1983\",\"128736\",\"750\",\"131469\",\"Included\"",
		"\"2026-06-25T11:13:15.544Z\",\"agent-2\",\"auto-2\",\"Included\",\"auto\",\"No\",\"0\",\"1983\",\"128736\",\"750\",\"not-a-number\",\"Included\"",
	}, "\n")

	result, err := ImportCursorUsageCSV(100, 1, "usage-events.csv", []byte(csvContent))
	if err != nil {
		t.Fatalf("ImportCursorUsageCSV returned error: %v", err)
	}
	if result.Batch.TotalRows != 2 {
		t.Fatalf("result.Batch.TotalRows = %d, want 2", result.Batch.TotalRows)
	}
	if result.Batch.ImportedRows != 1 {
		t.Fatalf("result.Batch.ImportedRows = %d, want 1", result.Batch.ImportedRows)
	}
	if result.Batch.RejectedRows != 1 {
		t.Fatalf("result.Batch.RejectedRows = %d, want 1", result.Batch.RejectedRows)
	}
	if result.Batch.IgnoredRows != 0 {
		t.Fatalf("result.Batch.IgnoredRows = %d, want 0", result.Batch.IgnoredRows)
	}

	var eventCount int64
	if err := model.DB.Model(&model.ExternalUsageCursorEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("Count(cursor events) error: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("eventCount = %d, want 1", eventCount)
	}
}

func TestNormalizeExternalModelName_UsesConfiguredMapping(t *testing.T) {
	setupExternalUsageTestTables(t)

	mapping := &model.ExternalUsageModelMapping{
		Source:              model.ExternalUsageSourceCursor,
		SourceModelName:     "auto",
		NormalizedModelName: "gateway:gpt-5",
		Status:              model.ExternalUsageStatusActive,
	}
	if err := model.DB.Create(mapping).Error; err != nil {
		t.Fatalf("Create(mapping) error: %v", err)
	}

	if got := normalizeExternalModelName(model.ExternalUsageSourceCursor, "auto"); got != "gateway:gpt-5" {
		t.Fatalf("normalizeExternalModelName() = %q, want %q", got, "gateway:gpt-5")
	}
}

func TestDeleteCursorImportBatch_RevertsImportedUsage(t *testing.T) {
	setupExternalUsageTestTables(t)

	batchToDelete := &model.ExternalUsageCursorImportBatch{
		UserID:     1,
		ImportedBy: 100,
		FileName:   "batch-delete.csv",
		FileSHA256: "hash-delete",
		Source:     model.ExternalUsageSourceCursor,
		Status:     model.ExternalUsageStatusActive,
	}
	if err := model.DB.Create(batchToDelete).Error; err != nil {
		t.Fatalf("Create(batchToDelete) error: %v", err)
	}

	remainingBatch := &model.ExternalUsageCursorImportBatch{
		UserID:     1,
		ImportedBy: 100,
		FileName:   "batch-keep.csv",
		FileSHA256: "hash-keep",
		Source:     model.ExternalUsageSourceCursor,
		Status:     model.ExternalUsageStatusActive,
	}
	if err := model.DB.Create(remainingBatch).Error; err != nil {
		t.Fatalf("Create(remainingBatch) error: %v", err)
	}

	events := []*model.ExternalUsageCursorEvent{
		{
			UserID:              1,
			BatchID:             batchToDelete.Id,
			Source:              model.ExternalUsageSourceCursor,
			EventIdentity:       "delete-1",
			OccurredAt:          1717200100,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          10,
			CacheReadToken:      20,
			OutputToken:         5,
			TotalToken:          35,
		},
		{
			UserID:              1,
			BatchID:             batchToDelete.Id,
			Source:              model.ExternalUsageSourceCursor,
			EventIdentity:       "delete-2",
			OccurredAt:          1717200200,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          3,
			CacheReadToken:      4,
			OutputToken:         2,
			TotalToken:          9,
		},
		{
			UserID:              1,
			BatchID:             remainingBatch.Id,
			Source:              model.ExternalUsageSourceCursor,
			EventIdentity:       "keep-1",
			OccurredAt:          1717200300,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          7,
			CacheReadToken:      1,
			OutputToken:         2,
			TotalToken:          10,
		},
	}

	for _, event := range events {
		if err := model.DB.Create(event).Error; err != nil {
			t.Fatalf("Create(event %s) error: %v", event.EventIdentity, err)
		}
		if err := model.ApplyExternalUsageAggregateDelta(model.DB, model.ExternalUsageAggregateDelta{
			UserID:              event.UserID,
			Origin:              model.ExternalUsageOriginCursorImport,
			Source:              event.Source,
			NormalizedModelName: event.NormalizedModelName,
			OccurredAt:          event.OccurredAt,
			EventCount:          1,
			InputTokens:         int64(event.InputCacheWriteToken + event.InputToken),
			CachedInputTokens:   int64(event.CacheReadToken),
			OutputTokens:        int64(event.OutputToken),
			TotalTokens:         int64(event.TotalToken),
		}); err != nil {
			t.Fatalf("ApplyExternalUsageAggregateDelta(%s) error: %v", event.EventIdentity, err)
		}
	}

	result, err := DeleteCursorImportBatch(batchToDelete.Id)
	if err != nil {
		t.Fatalf("DeleteCursorImportBatch returned error: %v", err)
	}
	if result.RevertedEventCount != 2 {
		t.Fatalf("result.RevertedEventCount = %d, want 2", result.RevertedEventCount)
	}

	var refreshedBatch model.ExternalUsageCursorImportBatch
	if err := model.DB.First(&refreshedBatch, batchToDelete.Id).Error; err != nil {
		t.Fatalf("First(batchToDelete) error: %v", err)
	}
	if refreshedBatch.Status != model.ExternalUsageStatusReverted {
		t.Fatalf("refreshedBatch.Status = %q, want %q", refreshedBatch.Status, model.ExternalUsageStatusReverted)
	}
	if refreshedBatch.RevertedAt <= 0 {
		t.Fatalf("refreshedBatch.RevertedAt = %d, want > 0", refreshedBatch.RevertedAt)
	}

	var revertedBatchEventCount int64
	if err := model.DB.Model(&model.ExternalUsageCursorEvent{}).
		Where("batch_id = ? AND status = ?", batchToDelete.Id, model.ExternalUsageStatusReverted).
		Count(&revertedBatchEventCount).Error; err != nil {
		t.Fatalf("Count(deleted batch events) error: %v", err)
	}
	if revertedBatchEventCount != 2 {
		t.Fatalf("revertedBatchEventCount = %d, want 2", revertedBatchEventCount)
	}

	summary, err := model.ListExternalUsageSummaryByUser(1, 0, 0)
	if err != nil {
		t.Fatalf("ListExternalUsageSummaryByUser returned error: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("len(summary) = %d, want 1", len(summary))
	}
	if summary[0].TotalTokens != 10 {
		t.Fatalf("summary[0].TotalTokens = %d, want 10", summary[0].TotalTokens)
	}
	if summary[0].EventCount != 1 {
		t.Fatalf("summary[0].EventCount = %d, want 1", summary[0].EventCount)
	}
}

func TestReplayCursorImportBatch_RestoresImportedUsage(t *testing.T) {
	setupExternalUsageTestTables(t)

	batch := &model.ExternalUsageCursorImportBatch{
		UserID:     1,
		ImportedBy: 100,
		FileName:   "batch-replay.csv",
		FileSHA256: "hash-replay",
		Source:     model.ExternalUsageSourceCursor,
		Status:     model.ExternalUsageStatusReverted,
		RevertedAt: time.Now().Unix(),
	}
	if err := model.DB.Create(batch).Error; err != nil {
		t.Fatalf("Create(batch) error: %v", err)
	}

	events := []*model.ExternalUsageCursorEvent{
		{
			UserID:              1,
			BatchID:             batch.Id,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusReverted,
			EventIdentity:       "replay-1",
			OccurredAt:          1717200100,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          6,
			CacheReadToken:      4,
			OutputToken:         2,
			TotalToken:          12,
			RevertedAt:          time.Now().Unix(),
		},
		{
			UserID:              1,
			BatchID:             batch.Id,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusReverted,
			EventIdentity:       "replay-2",
			OccurredAt:          1717200200,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          3,
			CacheReadToken:      2,
			OutputToken:         1,
			TotalToken:          6,
			RevertedAt:          time.Now().Unix(),
		},
	}
	for _, event := range events {
		if err := model.DB.Create(event).Error; err != nil {
			t.Fatalf("Create(event %s) error: %v", event.EventIdentity, err)
		}
	}

	result, err := ReplayCursorImportBatch(batch.Id)
	if err != nil {
		t.Fatalf("ReplayCursorImportBatch returned error: %v", err)
	}
	if result.ReactivatedEventCount != 2 {
		t.Fatalf("result.ReactivatedEventCount = %d, want 2", result.ReactivatedEventCount)
	}

	var refreshedBatch model.ExternalUsageCursorImportBatch
	if err := model.DB.First(&refreshedBatch, batch.Id).Error; err != nil {
		t.Fatalf("First(batch) error: %v", err)
	}
	if refreshedBatch.Status != model.ExternalUsageStatusActive {
		t.Fatalf("refreshedBatch.Status = %q, want %q", refreshedBatch.Status, model.ExternalUsageStatusActive)
	}

	summary, err := model.ListExternalUsageSummaryByUser(1, 0, 0)
	if err != nil {
		t.Fatalf("ListExternalUsageSummaryByUser returned error: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("len(summary) = %d, want 1", len(summary))
	}
	if summary[0].TotalTokens != 18 {
		t.Fatalf("summary[0].TotalTokens = %d, want 18", summary[0].TotalTokens)
	}
	if summary[0].EventCount != 2 {
		t.Fatalf("summary[0].EventCount = %d, want 2", summary[0].EventCount)
	}
}

func TestRebuildExternalUsageAggregates_RecomputesCursorAndClientFacts(t *testing.T) {
	setupExternalUsageTestTables(t)

	cursorEvent := &model.ExternalUsageCursorEvent{
		UserID:              1,
		BatchID:             1,
		Source:              model.ExternalUsageSourceCursor,
		Status:              model.ExternalUsageStatusActive,
		EventIdentity:       "cursor-1",
		OccurredAt:          1717200100,
		SourceModelName:     "auto",
		NormalizedModelName: "cursor:auto",
		InputToken:          8,
		CacheReadToken:      5,
		OutputToken:         2,
		TotalToken:          15,
	}
	if err := model.DB.Create(cursorEvent).Error; err != nil {
		t.Fatalf("Create(cursorEvent) error: %v", err)
	}

	clientEvent := &model.ExternalUsageClientEvent{
		UserID:               1,
		DeviceID:             1,
		BatchID:              1,
		Source:               model.ExternalUsageSourceCodex,
		Status:               model.ExternalUsageStatusAccepted,
		EventIdentity:        "client-1",
		OccurredAt:           1717200300,
		SourceModelName:      "gpt-5-codex",
		NormalizedModelName:  "codex:gpt-5-codex",
		InputToken:           11,
		CachedInputToken:     7,
		OutputToken:          3,
		ReasoningOutputToken: 1,
		TotalToken:           21,
	}
	if err := model.DB.Create(clientEvent).Error; err != nil {
		t.Fatalf("Create(clientEvent) error: %v", err)
	}

	staleAggregate := &model.ExternalUsageAggregate{
		UserID:              1,
		Origin:              model.ExternalUsageOriginCursorImport,
		Source:              model.ExternalUsageSourceCursor,
		NormalizedModelName: "cursor:stale",
		BucketAt:            1717200000,
		EventCount:          99,
		TotalTokens:         999,
	}
	if err := model.DB.Create(staleAggregate).Error; err != nil {
		t.Fatalf("Create(staleAggregate) error: %v", err)
	}

	result, err := RebuildExternalUsageAggregates(RebuildExternalUsageAggregatesParams{
		UserID: 1,
	})
	if err != nil {
		t.Fatalf("RebuildExternalUsageAggregates returned error: %v", err)
	}
	if result.RebuiltAggregateCount != 2 {
		t.Fatalf("result.RebuiltAggregateCount = %d, want 2", result.RebuiltAggregateCount)
	}

	summary, err := model.ListExternalUsageSummaryByUser(1, 0, 0)
	if err != nil {
		t.Fatalf("ListExternalUsageSummaryByUser returned error: %v", err)
	}
	if len(summary) != 2 {
		t.Fatalf("len(summary) = %d, want 2", len(summary))
	}
	if summary[0].TotalTokens+summary[1].TotalTokens != 36 {
		t.Fatalf("summary total tokens = %d, want 36", summary[0].TotalTokens+summary[1].TotalTokens)
	}
}

func TestRebuildExternalUsageAggregates_AppliesModelMappings(t *testing.T) {
	setupExternalUsageTestTables(t)

	cursorEvent := &model.ExternalUsageCursorEvent{
		UserID:              1,
		BatchID:             1,
		Source:              model.ExternalUsageSourceCursor,
		Status:              model.ExternalUsageStatusActive,
		EventIdentity:       "cursor-mapped-1",
		OccurredAt:          1717200100,
		SourceModelName:     "auto",
		NormalizedModelName: "cursor:auto",
		InputToken:          8,
		CacheReadToken:      5,
		OutputToken:         2,
		TotalToken:          15,
	}
	if err := model.DB.Create(cursorEvent).Error; err != nil {
		t.Fatalf("Create(cursorEvent) error: %v", err)
	}
	if err := model.DB.Create(&model.ExternalUsageModelMapping{
		Source:              model.ExternalUsageSourceCursor,
		SourceModelName:     "auto",
		NormalizedModelName: "gateway:gpt-5",
		Status:              model.ExternalUsageStatusActive,
	}).Error; err != nil {
		t.Fatalf("Create(mapping) error: %v", err)
	}

	if _, err := RebuildExternalUsageAggregates(RebuildExternalUsageAggregatesParams{
		UserID: 1,
	}); err != nil {
		t.Fatalf("RebuildExternalUsageAggregates returned error: %v", err)
	}

	summary, err := model.ListExternalUsageSummaryByUser(1, 0, 0)
	if err != nil {
		t.Fatalf("ListExternalUsageSummaryByUser returned error: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("len(summary) = %d, want 1", len(summary))
	}
	if summary[0].NormalizedModelName != "gateway:gpt-5" {
		t.Fatalf("summary[0].NormalizedModelName = %q, want %q", summary[0].NormalizedModelName, "gateway:gpt-5")
	}

	var refreshed model.ExternalUsageCursorEvent
	if err := model.DB.First(&refreshed, cursorEvent.Id).Error; err != nil {
		t.Fatalf("First(cursorEvent) error: %v", err)
	}
	if refreshed.NormalizedModelName != "gateway:gpt-5" {
		t.Fatalf("refreshed.NormalizedModelName = %q, want %q", refreshed.NormalizedModelName, "gateway:gpt-5")
	}
}

func TestListExternalUsageReportBatches_IncludesDeviceName(t *testing.T) {
	setupExternalUsageTestTables(t)

	device := &model.ExternalUsageDevice{
		UserID:           1,
		DeviceName:       "MacBook Pro",
		CredentialHash:   "credential-hash",
		CredentialPrefix: "credential-prefix",
		Status:           model.ExternalUsageStatusActive,
	}
	if err := device.SetAllowedSources([]string{model.ExternalUsageSourceCodex}); err != nil {
		t.Fatalf("device.SetAllowedSources error: %v", err)
	}
	if err := model.DB.Create(device).Error; err != nil {
		t.Fatalf("Create(device) error: %v", err)
	}

	batch := &model.ExternalUsageReportBatch{
		UserID:         1,
		DeviceID:       device.Id,
		Source:         model.ExternalUsageSourceCodex,
		Status:         model.ExternalUsageStatusAccepted,
		ClientVersion:  "1.0.0",
		AcceptedCount:  2,
		DuplicateCount: 1,
		RejectedCount:  0,
		OccurredFrom:   1717200000,
		OccurredTo:     1717200600,
	}
	if err := model.DB.Create(batch).Error; err != nil {
		t.Fatalf("Create(batch) error: %v", err)
	}

	rows, total, err := ListExternalUsageReportBatches(1, 0, 20)
	if err != nil {
		t.Fatalf("ListExternalUsageReportBatches returned error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].DeviceName != "MacBook Pro" {
		t.Fatalf("rows[0].DeviceName = %q, want %q", rows[0].DeviceName, "MacBook Pro")
	}
}

func TestListExternalUsageDetails_MergesCursorAndClientFacts(t *testing.T) {
	setupExternalUsageTestTables(t)

	cursorEvent := &model.ExternalUsageCursorEvent{
		UserID:              1,
		BatchID:             10,
		Source:              model.ExternalUsageSourceCursor,
		Status:              model.ExternalUsageStatusActive,
		EventIdentity:       "cursor-detail-1",
		OccurredAt:          1717200000,
		SourceModelName:     "auto",
		NormalizedModelName: "cursor:auto",
		InputToken:          4,
		CacheReadToken:      7,
		OutputToken:         3,
		TotalToken:          14,
	}
	if err := model.DB.Create(cursorEvent).Error; err != nil {
		t.Fatalf("Create(cursorEvent) error: %v", err)
	}

	clientEvent := &model.ExternalUsageClientEvent{
		UserID:               1,
		DeviceID:             2,
		BatchID:              20,
		Source:               model.ExternalUsageSourceCodex,
		Status:               model.ExternalUsageStatusAccepted,
		EventIdentity:        "client-detail-1",
		OccurredAt:           1717200300,
		SourceModelName:      "gpt-5-codex",
		NormalizedModelName:  "codex:gpt-5-codex",
		InputToken:           8,
		CachedInputToken:     5,
		OutputToken:          2,
		ReasoningOutputToken: 1,
		TotalToken:           16,
	}
	if err := model.DB.Create(clientEvent).Error; err != nil {
		t.Fatalf("Create(clientEvent) error: %v", err)
	}

	rows, total, err := ListExternalUsageDetails(ListExternalUsageDetailsParams{
		UserID: 1,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListExternalUsageDetails returned error: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Origin != model.ExternalUsageOriginClientReport {
		t.Fatalf("rows[0].Origin = %q, want %q", rows[0].Origin, model.ExternalUsageOriginClientReport)
	}
	if rows[1].Origin != model.ExternalUsageOriginCursorImport {
		t.Fatalf("rows[1].Origin = %q, want %q", rows[1].Origin, model.ExternalUsageOriginCursorImport)
	}
	if rows[0].TotalTokens != 16 {
		t.Fatalf("rows[0].TotalTokens = %d, want 16", rows[0].TotalTokens)
	}
	if rows[1].TotalTokens != 14 {
		t.Fatalf("rows[1].TotalTokens = %d, want 14", rows[1].TotalTokens)
	}
}

func TestListExternalUsageDetails_PaginatesMergedHistory(t *testing.T) {
	setupExternalUsageTestTables(t)

	cursorEvents := []*model.ExternalUsageCursorEvent{
		{
			UserID:              1,
			BatchID:             11,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusActive,
			EventIdentity:       "cursor-page-1",
			OccurredAt:          1717200000,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          1,
			CacheReadToken:      2,
			OutputToken:         1,
			TotalToken:          4,
		},
		{
			UserID:              1,
			BatchID:             12,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusActive,
			EventIdentity:       "cursor-page-2",
			OccurredAt:          1717199800,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          2,
			CacheReadToken:      2,
			OutputToken:         1,
			TotalToken:          5,
		},
		{
			UserID:              1,
			BatchID:             13,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusActive,
			EventIdentity:       "cursor-page-3",
			OccurredAt:          1717199600,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          3,
			CacheReadToken:      1,
			OutputToken:         1,
			TotalToken:          5,
		},
	}
	for _, event := range cursorEvents {
		if err := model.DB.Create(event).Error; err != nil {
			t.Fatalf("Create(cursor event %s) error: %v", event.EventIdentity, err)
		}
	}

	clientEvents := []*model.ExternalUsageClientEvent{
		{
			UserID:              1,
			DeviceID:            2,
			BatchID:             21,
			Source:              model.ExternalUsageSourceCodex,
			Status:              model.ExternalUsageStatusAccepted,
			EventIdentity:       "client-page-1",
			OccurredAt:          1717199900,
			SourceModelName:     "gpt-5-codex",
			NormalizedModelName: "codex:gpt-5-codex",
			InputToken:          4,
			CachedInputToken:    1,
			OutputToken:         1,
			TotalToken:          6,
		},
		{
			UserID:              1,
			DeviceID:            2,
			BatchID:             22,
			Source:              model.ExternalUsageSourceCodex,
			Status:              model.ExternalUsageStatusAccepted,
			EventIdentity:       "client-page-2",
			OccurredAt:          1717199700,
			SourceModelName:     "gpt-5-codex",
			NormalizedModelName: "codex:gpt-5-codex",
			InputToken:          5,
			CachedInputToken:    1,
			OutputToken:         1,
			TotalToken:          7,
		},
	}
	for _, event := range clientEvents {
		if err := model.DB.Create(event).Error; err != nil {
			t.Fatalf("Create(client event %s) error: %v", event.EventIdentity, err)
		}
	}

	rows, total, err := ListExternalUsageDetails(ListExternalUsageDetailsParams{
		UserID: 1,
		Offset: 2,
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("ListExternalUsageDetails returned error: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].EventIdentity != "cursor-page-2" {
		t.Fatalf("rows[0].EventIdentity = %q, want %q", rows[0].EventIdentity, "cursor-page-2")
	}
	if rows[1].EventIdentity != "client-page-2" {
		t.Fatalf("rows[1].EventIdentity = %q, want %q", rows[1].EventIdentity, "client-page-2")
	}
}
