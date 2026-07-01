package usagereporter

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCollectUsageDispatchesCodexCollector(t *testing.T) {
	tempDir := t.TempDir()
	sessionDir := filepath.Join(tempDir, "sessions", "2026", "06", "18")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join("testdata", "codex-session.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "session.jsonl"), content, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	result, err := CollectUsage(SourceCodex, tempDir, NewState(), 0)
	if err != nil {
		t.Fatalf("CollectUsage error: %v", err)
	}
	if len(result.Events) == 0 {
		t.Fatal("CollectUsage returned no events")
	}
}

func TestCollectUsageRejectsUnsupportedSourceCollectors(t *testing.T) {
	for _, source := range []string{"zcode", "minimax_code"} {
		if _, err := CollectUsage(source, t.TempDir(), NewState(), 0); err == nil {
			t.Fatalf("CollectUsage(%q) error = nil, want non-nil", source)
		}
	}
}

func TestCollectUsage_MissingCodexDirReturnsNoEvents(t *testing.T) {
	result, err := CollectUsage(SourceCodex, filepath.Join(t.TempDir(), "missing"), NewState(), 0)
	if err != nil {
		t.Fatalf("CollectUsage error: %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("len(result.Events) = %d, want 0", len(result.Events))
	}
}

func TestCollectUsagePreservedStateRemainsIncrementalAfterReload(t *testing.T) {
	tempDir := t.TempDir()
	sessionDir := filepath.Join(tempDir, "sessions", "2026", "06", "18")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join("testdata", "codex-session.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "session.jsonl"), content, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	state := NewState()
	first, err := CollectUsage(SourceCodex, tempDir, state, 0)
	if err != nil {
		t.Fatalf("CollectUsage first error: %v", err)
	}
	if len(first.Events) == 0 {
		t.Fatal("CollectUsage first returned no events")
	}

	acceptedIDs := make([]string, 0, len(first.Events))
	for _, event := range first.Events {
		acceptedIDs = append(acceptedIDs, event.EventID)
	}
	ApplyAcknowledgements(state, first.NextState, &ReportAcknowledgement{AcceptedEventIDs: acceptedIDs})
	state.Server = "https://same.example"

	statePath := filepath.Join(tempDir, "state.json")
	if err := state.Save(statePath); err != nil {
		t.Fatalf("state.Save error: %v", err)
	}
	reloadedState, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	reloadedState = reloadedState.PrepareForServer("https://same.example/")

	second, err := CollectUsage(SourceCodex, tempDir, reloadedState, 0)
	if err != nil {
		t.Fatalf("CollectUsage second error: %v", err)
	}
	if len(second.Events) != 0 {
		t.Fatalf("len(second.Events) = %d, want 0", len(second.Events))
	}
}

func TestCollectUsageServerSwitchReplaysHistoricalEvents(t *testing.T) {
	tempDir := t.TempDir()
	sessionDir := filepath.Join(tempDir, "sessions", "2026", "06", "18")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join("testdata", "codex-session.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "session.jsonl"), content, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	state := NewState()
	first, err := CollectUsage(SourceCodex, tempDir, state, 0)
	if err != nil {
		t.Fatalf("CollectUsage first error: %v", err)
	}
	if len(first.Events) == 0 {
		t.Fatal("CollectUsage first returned no events")
	}

	acceptedIDs := make([]string, 0, len(first.Events))
	for _, event := range first.Events {
		acceptedIDs = append(acceptedIDs, event.EventID)
	}
	ApplyAcknowledgements(state, first.NextState, &ReportAcknowledgement{AcceptedEventIDs: acceptedIDs})
	state.Server = "https://old.example"

	switchState := state.PrepareForServer("https://new.example")
	if switchState.Server != "https://new.example" {
		t.Fatalf("switchState.Server = %q, want %q", switchState.Server, "https://new.example")
	}
	second, err := CollectUsage(SourceCodex, tempDir, switchState, 0)
	if err != nil {
		t.Fatalf("CollectUsage second error: %v", err)
	}
	if len(second.Events) != len(first.Events) {
		t.Fatalf("len(second.Events) = %d, want %d", len(second.Events), len(first.Events))
	}
}

func TestCollectUsageFiltersEventsOlderThanConfiguredWindow(t *testing.T) {
	tempDir := t.TempDir()
	sessionDir := filepath.Join(tempDir, "sessions", "2026", "06", "30")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	now := time.Now().UTC()
	oldTimestamp := now.Add(-91 * 24 * time.Hour).Format(time.RFC3339Nano)
	freshTimestamp := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	content := []byte("{\"timestamp\":\"" + oldTimestamp + "\",\"type\":\"session_meta\",\"payload\":{\"session_id\":\"window-session-1\",\"cli_version\":\"0.142.4\"}}\n" +
		"{\"timestamp\":\"" + oldTimestamp + "\",\"type\":\"turn_context\",\"payload\":{\"turn_id\":\"turn-1\",\"model\":\"gpt-5.4\"}}\n" +
		"{\"timestamp\":\"" + oldTimestamp + "\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":100,\"cached_input_tokens\":80,\"output_tokens\":20,\"reasoning_output_tokens\":0,\"total_tokens\":120}}}}\n" +
		"{\"timestamp\":\"" + freshTimestamp + "\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":150,\"cached_input_tokens\":110,\"output_tokens\":40,\"reasoning_output_tokens\":0,\"total_tokens\":190}}}}\n")
	if err := os.WriteFile(filepath.Join(sessionDir, "windowed.jsonl"), content, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	result, err := CollectUsage(SourceCodex, tempDir, NewState(), 90)
	if err != nil {
		t.Fatalf("CollectUsage error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("len(result.Events) = %d, want 1", len(result.Events))
	}
	if result.Events[0].OccurredAt <= now.Add(-90*24*time.Hour).Unix() {
		t.Fatalf("result.Events[0].OccurredAt = %d, want within accept window", result.Events[0].OccurredAt)
	}
	if result.Events[0].TotalTokens != 70 {
		t.Fatalf("result.Events[0].TotalTokens = %d, want 70", result.Events[0].TotalTokens)
	}
}
