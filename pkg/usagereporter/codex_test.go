package usagereporter

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func copyCodexFixture(t *testing.T, dir string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("testdata", "codex-session.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile fixture error: %v", err)
	}
	target := filepath.Join(dir, "sessions", "2026", "06", "18", "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatalf("WriteFile target error: %v", err)
	}
	return target
}

func writeCodexFixture(t *testing.T, target string, content string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile target error: %v", err)
	}
	return target
}

func offsetAfterLine(t *testing.T, path string, lineCount int) int64 {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error: %v", path, err)
	}
	offset := 0
	for i := 0; i < lineCount; i++ {
		idx := bytes.IndexByte(content[offset:], '\n')
		if idx < 0 {
			t.Fatalf("fixture has fewer than %d lines", lineCount)
		}
		offset += idx + 1
	}
	return int64(offset)
}

func TestCollectCodexUsageParsesTokenDeltas(t *testing.T) {
	root := t.TempDir()
	copyCodexFixture(t, root)

	result, err := CollectCodexUsage(root, NewState())
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if result.ClientVersion != "0.142.2" {
		t.Fatalf("result.ClientVersion = %q, want %q", result.ClientVersion, "0.142.2")
	}
	if len(result.Events) != 2 {
		t.Fatalf("len(result.Events) = %d, want 2", len(result.Events))
	}

	first := result.Events[0]
	if first.SourceModelName != "gpt-5-codex" {
		t.Fatalf("first.SourceModelName = %q, want %q", first.SourceModelName, "gpt-5-codex")
	}
	if first.TotalTokens != 110 {
		t.Fatalf("first.TotalTokens = %d, want 110", first.TotalTokens)
	}
	if first.InputTokens != 100 || first.CachedInputTokens != 60 || first.OutputTokens != 10 || first.ReasoningOutputTokens != 3 {
		t.Fatalf("unexpected first token breakdown: %+v", first)
	}
	if strings.Contains(first.SessionID, "session-plain-1") {
		t.Fatalf("first.SessionID leaked raw session id: %q", first.SessionID)
	}
	if strings.Contains(first.EventID, "/Users/noah/private/project-alpha") {
		t.Fatalf("first.EventID leaked raw cwd: %q", first.EventID)
	}

	second := result.Events[1]
	if second.TotalTokens != 60 {
		t.Fatalf("second.TotalTokens = %d, want 60", second.TotalTokens)
	}
	if second.InputTokens != 50 || second.CachedInputTokens != 30 || second.OutputTokens != 10 || second.ReasoningOutputTokens != 2 {
		t.Fatalf("unexpected second token breakdown: %+v", second)
	}

	if len(result.NextState.Files) != 1 {
		t.Fatalf("len(result.NextState.Files) = %d, want 1", len(result.NextState.Files))
	}
	if len(result.NextState.SessionTotals) != 1 {
		t.Fatalf("len(result.NextState.SessionTotals) = %d, want 1", len(result.NextState.SessionTotals))
	}
	for _, totals := range result.NextState.SessionTotals {
		if totals.TotalTokens != 170 {
			t.Fatalf("totals.TotalTokens = %d, want 170", totals.TotalTokens)
		}
	}
}

func TestCollectCodexUsageHonorsOffsetsAndSavedTotals(t *testing.T) {
	root := t.TempDir()
	target := copyCodexFixture(t, root)
	offset := offsetAfterLine(t, target, 4)

	state := NewState()
	state.Files[target] = FileState{
		Offset:     offset,
		SessionID:  "session-plain-1",
		CLIVersion: "0.142.2",
		TurnID:     "turn-2",
		Model:      "gpt-5-codex",
	}
	state.SessionTotals[sha256Hex("session-plain-1")] = TokenUsageTotals{
		InputTokens:           100,
		CachedInputTokens:     60,
		OutputTokens:          10,
		ReasoningOutputTokens: 3,
		TotalTokens:           110,
	}

	result, err := CollectCodexUsage(root, state)
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("len(result.Events) = %d, want 1", len(result.Events))
	}
	if result.Events[0].TotalTokens != 60 {
		t.Fatalf("result.Events[0].TotalTokens = %d, want 60", result.Events[0].TotalTokens)
	}
	if result.Events[0].SourceModelName != "gpt-5-codex" {
		t.Fatalf("result.Events[0].SourceModelName = %q, want %q", result.Events[0].SourceModelName, "gpt-5-codex")
	}
}

func TestCollectCodexUsageLegacyStateRescansToRecoverContext(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "sessions", "2026", "06", "30", "legacy-resume.jsonl")
	writeCodexFixture(t, target, strings.Join([]string{
		`{"timestamp":"2026-06-30T10:40:00.000Z","type":"session_meta","payload":{"session_id":"legacy-session-1","cli_version":"0.142.3"}}`,
		`{"timestamp":"2026-06-30T10:40:01.000Z","type":"turn_context","payload":{"turn_id":"turn-1","model":"codex-auto-review"}}`,
		`{"timestamp":"2026-06-30T10:40:02.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":90,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":110}}}}`,
		`{"timestamp":"2026-06-30T10:45:01.000Z","type":"turn_context","payload":{"turn_id":"turn-2","model":"codex-auto-review"}}`,
		`{"timestamp":"2026-06-30T10:45:02.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":240,"cached_input_tokens":200,"output_tokens":30,"reasoning_output_tokens":0,"total_tokens":270}}}}`,
		"",
	}, "\n"))

	state := NewState()
	state.Files[target] = FileState{
		Offset:     offsetAfterLine(t, target, 4),
		SessionID:  "legacy-session-1",
		CLIVersion: "0.142.3",
	}
	state.SessionTotals[sha256Hex("legacy-session-1")] = TokenUsageTotals{
		InputTokens:       100,
		CachedInputTokens: 90,
		OutputTokens:      10,
		TotalTokens:       110,
	}
	firstEventID := sha256Hex(fmt.Sprintf("%s|%s|%d|%d", sha256Hex("legacy-session-1"), "turn-1", parseOccurredAt("2026-06-30T10:40:02.000Z"), 110))
	state.Acknowledged[firstEventID] = 1719730000

	result, err := CollectCodexUsage(root, state)
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("len(result.Events) = %d, want 1", len(result.Events))
	}
	if result.Events[0].SourceModelName != "codex-auto-review" {
		t.Fatalf("result.Events[0].SourceModelName = %q, want %q", result.Events[0].SourceModelName, "codex-auto-review")
	}
	if result.Events[0].TotalTokens != 160 {
		t.Fatalf("result.Events[0].TotalTokens = %d, want 160", result.Events[0].TotalTokens)
	}
}

func TestCollectCodexUsageCarriesSessionContextAcrossSplitFiles(t *testing.T) {
	root := t.TempDir()
	writeCodexFixture(t, filepath.Join(root, "sessions", "2026", "05", "07", "parent.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-05-07T13:27:43.260Z","type":"session_meta","payload":{"id":"parent-session-1","cli_version":"0.128.0-alpha.1"}}`,
		`{"timestamp":"2026-05-07T13:27:43.264Z","type":"turn_context","payload":{"turn_id":"turn-1","model":"gpt-5.5"}}`,
		`{"timestamp":"2026-05-07T13:27:45.101Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":60,"output_tokens":40,"reasoning_output_tokens":0,"total_tokens":140}}}}`,
		"",
	}, "\n"))
	writeCodexFixture(t, filepath.Join(root, "sessions", "2026", "05", "08", "split-subagent.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-05-08T03:15:41.799Z","type":"session_meta","payload":{"id":"subagent-session-1","forked_from_id":"parent-session-1","cli_version":"0.129.0-alpha.15"}}`,
		`{"timestamp":"2026-05-08T03:15:41.801Z","type":"session_meta","payload":{"id":"parent-session-1","cli_version":"0.128.0-alpha.1"}}`,
		`{"timestamp":"2026-05-08T03:15:41.801Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","started_at":1778162141}}`,
		`{"timestamp":"2026-05-08T03:15:41.802Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":90,"output_tokens":60,"reasoning_output_tokens":0,"total_tokens":210}}}}`,
		"",
	}, "\n"))

	result, err := CollectCodexUsage(root, NewState())
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("len(result.Events) = %d, want 2", len(result.Events))
	}
	if result.Events[0].SourceModelName != "gpt-5.5" {
		t.Fatalf("result.Events[0].SourceModelName = %q, want %q", result.Events[0].SourceModelName, "gpt-5.5")
	}
	if result.Events[1].SourceModelName != "gpt-5.5" {
		t.Fatalf("result.Events[1].SourceModelName = %q, want %q", result.Events[1].SourceModelName, "gpt-5.5")
	}
	if result.Events[1].TotalTokens != 70 {
		t.Fatalf("result.Events[1].TotalTokens = %d, want 70", result.Events[1].TotalTokens)
	}
	ctx, ok := result.NextState.SessionContexts["parent-session-1"]
	if !ok {
		t.Fatal("expected parent session context to be persisted")
	}
	if ctx.Model != "gpt-5.5" {
		t.Fatalf("ctx.Model = %q, want %q", ctx.Model, "gpt-5.5")
	}
}

func TestCollectCodexUsageRescansOldStateToRecoverSplitSessionContext(t *testing.T) {
	root := t.TempDir()
	parentPath := filepath.Join(root, "sessions", "2026", "05", "07", "parent.jsonl")
	splitPath := filepath.Join(root, "sessions", "2026", "05", "08", "split-subagent.jsonl")
	writeCodexFixture(t, parentPath, strings.Join([]string{
		`{"timestamp":"2026-05-07T13:27:43.260Z","type":"session_meta","payload":{"id":"parent-session-1","cli_version":"0.128.0-alpha.1"}}`,
		`{"timestamp":"2026-05-07T13:27:43.264Z","type":"turn_context","payload":{"turn_id":"turn-1","model":"gpt-5.5"}}`,
		`{"timestamp":"2026-05-07T13:27:45.101Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":60,"output_tokens":40,"reasoning_output_tokens":0,"total_tokens":140}}}}`,
		"",
	}, "\n"))
	writeCodexFixture(t, splitPath, strings.Join([]string{
		`{"timestamp":"2026-05-08T03:15:41.799Z","type":"session_meta","payload":{"id":"subagent-session-1","forked_from_id":"parent-session-1","cli_version":"0.129.0-alpha.15"}}`,
		`{"timestamp":"2026-05-08T03:15:41.801Z","type":"session_meta","payload":{"id":"parent-session-1","cli_version":"0.128.0-alpha.1"}}`,
		`{"timestamp":"2026-05-08T03:15:41.801Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","started_at":1778162141}}`,
		`{"timestamp":"2026-05-08T03:15:41.802Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":90,"output_tokens":60,"reasoning_output_tokens":0,"total_tokens":210}}}}`,
		"",
	}, "\n"))

	parentSessionHash := sha256Hex("parent-session-1")
	firstEventID := sha256Hex(fmt.Sprintf("%s|%s|%d|%d", parentSessionHash, "turn-1", parseOccurredAt("2026-05-07T13:27:45.101Z"), 140))
	state := &State{
		Files: map[string]FileState{
			parentPath: {
				Offset:     offsetAfterLine(t, parentPath, 3),
				SessionID:  "parent-session-1",
				CLIVersion: "0.128.0-alpha.1",
				TurnID:     "turn-1",
				Model:      "gpt-5.5",
			},
			splitPath: {
				Offset:     offsetAfterLine(t, splitPath, 4),
				SessionID:  "parent-session-1",
				CLIVersion: "0.128.0-alpha.1",
			},
		},
		SessionTotals: map[string]TokenUsageTotals{
			parentSessionHash: {
				InputTokens:       150,
				CachedInputTokens: 90,
				OutputTokens:      60,
				TotalTokens:       210,
			},
		},
		Acknowledged: map[string]int64{
			firstEventID: 1719730000,
		},
		Rejected: make(map[string]string),
	}

	result, err := CollectCodexUsage(root, state)
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("len(result.Events) = %d, want 1", len(result.Events))
	}
	if result.Events[0].SourceModelName != "gpt-5.5" {
		t.Fatalf("result.Events[0].SourceModelName = %q, want %q", result.Events[0].SourceModelName, "gpt-5.5")
	}
	if result.Events[0].TotalTokens != 70 {
		t.Fatalf("result.Events[0].TotalTokens = %d, want 70", result.Events[0].TotalTokens)
	}
	if result.NextState.SessionContextVersion != reporterStateSessionContextVersion {
		t.Fatalf("result.NextState.SessionContextVersion = %d, want %d", result.NextState.SessionContextVersion, reporterStateSessionContextVersion)
	}
}

func TestCollectCodexUsageBackfillsBufferedEventsWithFirstObservedModel(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "sessions", "2025", "12", "24", "legacy-buffered.jsonl")
	writeCodexFixture(t, target, strings.Join([]string{
		`{"timestamp":"2025-12-24T09:22:38.087Z","type":"session_meta","payload":{"id":"legacy-session-2","cli_version":"0.77.0"}}`,
		`{"timestamp":"2025-12-24T09:25:38.832Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":11454,"cached_input_tokens":3072,"output_tokens":62,"reasoning_output_tokens":0,"total_tokens":11516}}}}`,
		`{"timestamp":"2025-12-24T09:28:59.400Z","type":"turn_context","payload":{"model":"gpt-5.2-codex"}}`,
		`{"timestamp":"2025-12-24T09:29:06.233Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":12719,"cached_input_tokens":0,"output_tokens":166,"reasoning_output_tokens":128,"total_tokens":12885}}}}`,
		"",
	}, "\n"))

	result, err := CollectCodexUsage(root, NewState())
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("len(result.Events) = %d, want 2", len(result.Events))
	}
	if result.Events[0].SourceModelName != "gpt-5.2-codex" {
		t.Fatalf("result.Events[0].SourceModelName = %q, want %q", result.Events[0].SourceModelName, "gpt-5.2-codex")
	}
	if result.Events[1].SourceModelName != "gpt-5.2-codex" {
		t.Fatalf("result.Events[1].SourceModelName = %q, want %q", result.Events[1].SourceModelName, "gpt-5.2-codex")
	}
	if result.Events[1].TotalTokens != 1369 {
		t.Fatalf("result.Events[1].TotalTokens = %d, want 1369", result.Events[1].TotalTokens)
	}
}

func TestCollectCodexUsageSupportsLegacySessionMetaID(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "sessions", "2025", "12", "07", "legacy.jsonl")
	writeCodexFixture(t, target, strings.Join([]string{
		`{"timestamp":"2025-12-07T08:32:51.428Z","type":"session_meta","payload":{"id":"legacy-session-1","cli_version":"0.65.0"}}`,
		`{"timestamp":"2025-12-07T08:33:03.854Z","type":"turn_context","payload":{"turn_id":"turn-1","model":"gpt-5.1-codex-max"}}`,
		`{"timestamp":"2025-12-07T08:33:09.387Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5573,"cached_input_tokens":4992,"output_tokens":164,"reasoning_output_tokens":128,"total_tokens":5737}}}}`,
		"",
	}, "\n"))

	result, err := CollectCodexUsage(root, NewState())
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("len(result.Events) = %d, want 1", len(result.Events))
	}
	if result.Events[0].SessionID != sha256Hex("legacy-session-1") {
		t.Fatalf("result.Events[0].SessionID = %q, want hashed legacy session id", result.Events[0].SessionID)
	}
	if result.Events[0].TotalTokens != 5737 {
		t.Fatalf("result.Events[0].TotalTokens = %d, want 5737", result.Events[0].TotalTokens)
	}
}

func TestCollectCodexUsageIncludesArchivedSessionsAlongsideLiveSessions(t *testing.T) {
	root := t.TempDir()
	writeCodexFixture(t, filepath.Join(root, "sessions", "2026", "06", "29", "live.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-29T10:00:00.000Z","type":"session_meta","payload":{"session_id":"live-session-1","cli_version":"0.142.3"}}`,
		`{"timestamp":"2026-06-29T10:00:01.000Z","type":"turn_context","payload":{"turn_id":"turn-live","model":"gpt-5.4"}}`,
		`{"timestamp":"2026-06-29T10:00:02.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"reasoning_output_tokens":0,"total_tokens":120}}}}`,
		"",
	}, "\n"))
	writeCodexFixture(t, filepath.Join(root, "archived_sessions", "rollout-archived.jsonl"), strings.Join([]string{
		`{"timestamp":"2026-06-01T08:00:00.000Z","type":"session_meta","payload":{"session_id":"archived-session-1","cli_version":"0.142.0"}}`,
		`{"timestamp":"2026-06-01T08:00:01.000Z","type":"turn_context","payload":{"turn_id":"turn-archived","model":"gpt-5.4-mini"}}`,
		`{"timestamp":"2026-06-01T08:00:02.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":50,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":60}}}}`,
		"",
	}, "\n"))

	result, err := CollectCodexUsage(filepath.Join(root, "sessions"), NewState())
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("len(result.Events) = %d, want 2", len(result.Events))
	}

	totalTokens := 0
	models := make(map[string]bool)
	for _, event := range result.Events {
		totalTokens += event.TotalTokens
		models[event.SourceModelName] = true
	}
	if totalTokens != 180 {
		t.Fatalf("totalTokens = %d, want 180", totalTokens)
	}
	if !models["gpt-5.4"] || !models["gpt-5.4-mini"] {
		t.Fatalf("unexpected models found: %+v", models)
	}
}

func TestCollectCodexUsageDeduplicatesEquivalentEventsAcrossRolloutFiles(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		`{"timestamp":"2026-04-07T10:35:22.000Z","type":"session_meta","payload":{"session_id":"dup-session-1","cli_version":"0.128.0-alpha.1"}}`,
		`{"timestamp":"2026-04-07T10:35:22.100Z","type":"turn_context","payload":{"turn_id":"turn-dup-1","model":"gpt-5.5"}}`,
		`{"timestamp":"2026-04-07T10:35:23.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"reasoning_output_tokens":0,"total_tokens":120}}}}`,
		`{"timestamp":"2026-04-07T10:35:23.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"cached_input_tokens":120,"output_tokens":40,"reasoning_output_tokens":0,"total_tokens":200}}}}`,
		"",
	}, "\n")
	writeCodexFixture(t, filepath.Join(root, "sessions", "2026", "04", "07", "rollout-a.jsonl"), content)
	writeCodexFixture(t, filepath.Join(root, "sessions", "2026", "04", "07", "rollout-b.jsonl"), content)

	result, err := CollectCodexUsage(root, NewState())
	if err != nil {
		t.Fatalf("CollectCodexUsage error: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("len(result.Events) = %d, want 2", len(result.Events))
	}
	if result.Events[0].TotalTokens != 120 {
		t.Fatalf("result.Events[0].TotalTokens = %d, want 120", result.Events[0].TotalTokens)
	}
	if result.Events[1].TotalTokens != 80 {
		t.Fatalf("result.Events[1].TotalTokens = %d, want 80", result.Events[1].TotalTokens)
	}
}

func TestCollectCodexUsagePreservesHighestSessionTotalAcrossSplitFiles(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "sessions", "2026", "07", "01", "rollout-main.jsonl")
	splitPath := filepath.Join(root, "sessions", "2026", "07", "01", "rollout-split.jsonl")
	sessionID := "shared-session-1"

	writeCodexFixture(t, mainPath, strings.Join([]string{
		`{"timestamp":"2026-07-01T02:00:00.000Z","type":"session_meta","payload":{"session_id":"shared-session-1","cli_version":"0.142.3"}}`,
		`{"timestamp":"2026-07-01T02:00:01.000Z","type":"turn_context","payload":{"turn_id":"turn-main","model":"gpt-5.4"}}`,
		`{"timestamp":"2026-07-01T02:00:02.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"reasoning_output_tokens":0,"total_tokens":120}}}}`,
		`{"timestamp":"2026-07-01T02:00:03.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":180,"cached_input_tokens":140,"output_tokens":40,"reasoning_output_tokens":0,"total_tokens":220}}}}`,
		"",
	}, "\n"))
	writeCodexFixture(t, splitPath, strings.Join([]string{
		`{"timestamp":"2026-07-01T02:00:00.500Z","type":"session_meta","payload":{"session_id":"shared-session-1","cli_version":"0.142.3"}}`,
		`{"timestamp":"2026-07-01T02:00:01.500Z","type":"turn_context","payload":{"turn_id":"turn-main","model":"gpt-5.4"}}`,
		`{"timestamp":"2026-07-01T02:00:02.500Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":60,"cached_input_tokens":50,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":70}}}}`,
		`{"timestamp":"2026-07-01T02:00:03.500Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":70,"cached_input_tokens":60,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":80}}}}`,
		"",
	}, "\n"))

	first, err := CollectCodexUsage(root, NewState())
	if err != nil {
		t.Fatalf("CollectCodexUsage(first) error: %v", err)
	}
	if len(first.Events) != 2 {
		t.Fatalf("len(first.Events) = %d, want 2", len(first.Events))
	}
	sessionHash := sha256Hex(sessionID)
	if first.NextState.SessionTotals[sessionHash].TotalTokens != 220 {
		t.Fatalf("first.NextState.SessionTotals[%q].TotalTokens = %d, want 220", sessionHash, first.NextState.SessionTotals[sessionHash].TotalTokens)
	}

	writeCodexFixture(t, mainPath, strings.Join([]string{
		`{"timestamp":"2026-07-01T02:00:00.000Z","type":"session_meta","payload":{"session_id":"shared-session-1","cli_version":"0.142.3"}}`,
		`{"timestamp":"2026-07-01T02:00:01.000Z","type":"turn_context","payload":{"turn_id":"turn-main","model":"gpt-5.4"}}`,
		`{"timestamp":"2026-07-01T02:00:02.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"reasoning_output_tokens":0,"total_tokens":120}}}}`,
		`{"timestamp":"2026-07-01T02:00:03.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":180,"cached_input_tokens":140,"output_tokens":40,"reasoning_output_tokens":0,"total_tokens":220}}}}`,
		`{"timestamp":"2026-07-01T02:00:04.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":205,"cached_input_tokens":160,"output_tokens":45,"reasoning_output_tokens":0,"total_tokens":250}}}}`,
		"",
	}, "\n"))

	second, err := CollectCodexUsage(root, first.NextState)
	if err != nil {
		t.Fatalf("CollectCodexUsage(second) error: %v", err)
	}
	if len(second.Events) != 1 {
		t.Fatalf("len(second.Events) = %d, want 1", len(second.Events))
	}
	if second.Events[0].TotalTokens != 30 {
		t.Fatalf("second.Events[0].TotalTokens = %d, want 30", second.Events[0].TotalTokens)
	}
}

func TestReporterStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := NewState()
	state.Files["/tmp/example.jsonl"] = FileState{Offset: 123, SessionID: "session-1", CLIVersion: "0.142.2", TurnID: "turn-9", Model: "codex-auto-review"}
	state.SessionTotals["session-hash"] = TokenUsageTotals{TotalTokens: 45}
	state.Acknowledged["evt-1"] = 1718700000
	state.Rejected["evt-2"] = "bad payload"

	if err := state.Save(path); err != nil {
		t.Fatalf("state.Save error: %v", err)
	}
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if loaded.Files["/tmp/example.jsonl"].Offset != 123 {
		t.Fatalf("loaded file offset = %d, want 123", loaded.Files["/tmp/example.jsonl"].Offset)
	}
	if loaded.Files["/tmp/example.jsonl"].TurnID != "turn-9" {
		t.Fatalf("loaded turn id = %q, want %q", loaded.Files["/tmp/example.jsonl"].TurnID, "turn-9")
	}
	if loaded.Files["/tmp/example.jsonl"].Model != "codex-auto-review" {
		t.Fatalf("loaded model = %q, want %q", loaded.Files["/tmp/example.jsonl"].Model, "codex-auto-review")
	}
	if loaded.SessionTotals["session-hash"].TotalTokens != 45 {
		t.Fatalf("loaded total tokens = %d, want 45", loaded.SessionTotals["session-hash"].TotalTokens)
	}
	if loaded.Rejected["evt-2"] != "bad payload" {
		t.Fatalf("loaded rejected reason = %q, want %q", loaded.Rejected["evt-2"], "bad payload")
	}
	if loaded.SessionContextVersion != reporterStateSessionContextVersion {
		t.Fatalf("loaded state version = %d, want %d", loaded.SessionContextVersion, reporterStateSessionContextVersion)
	}
}

func TestApplyAcknowledgementsCommitsNextStateOnlyOnAck(t *testing.T) {
	current := NewState()
	next := NewState()
	next.Files["/tmp/rollout.jsonl"] = FileState{Offset: 200, SessionID: "session-1"}
	next.SessionTotals["session-hash"] = TokenUsageTotals{TotalTokens: 170}
	next.SessionContexts["session-1"] = SessionContext{TurnID: "turn-1", Model: "gpt-5.5"}

	ack := &ReportAcknowledgement{
		AcceptedEventIDs:  []string{"evt-1"},
		DuplicateEventIDs: []string{"evt-2"},
		Rejected: []RejectedEvent{
			{EventID: "evt-3", Reason: "bad payload"},
		},
	}
	ApplyAcknowledgements(current, next, ack)

	if current.Files["/tmp/rollout.jsonl"].Offset != 200 {
		t.Fatalf("current file offset = %d, want 200", current.Files["/tmp/rollout.jsonl"].Offset)
	}
	if _, ok := current.Acknowledged["evt-1"]; !ok {
		t.Fatal("accepted event was not acknowledged")
	}
	if _, ok := current.Acknowledged["evt-2"]; !ok {
		t.Fatal("duplicate event was not acknowledged")
	}
	if current.Rejected["evt-3"] != "bad payload" {
		t.Fatalf("rejected reason = %q, want %q", current.Rejected["evt-3"], "bad payload")
	}
	if current.SessionContexts["session-1"].Model != "gpt-5.5" {
		t.Fatalf("current session context model = %q, want %q", current.SessionContexts["session-1"].Model, "gpt-5.5")
	}
}

func TestUploadReportPostsExpectedPayload(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/api/external-usage/report" {
				t.Fatalf("r.URL.Path = %q, want %q", r.URL.Path, "/api/external-usage/report")
			}
			if got := r.Header.Get("Authorization"); got != "Bearer eur_test" {
				t.Fatalf("Authorization = %q, want %q", got, "Bearer eur_test")
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll body error: %v", err)
			}
			if !strings.Contains(string(body), "\"source\":\"codex\"") {
				t.Fatalf("request body missing source: %s", string(body))
			}
			if !strings.Contains(string(body), "\"event_id\":\"evt-1\"") {
				t.Fatalf("request body missing event id: %s", string(body))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"success":true,"data":{"accepted_event_ids":["evt-1"],"duplicate_event_ids":["evt-2"],"rejected":[{"event_id":"evt-3","reason":"bad payload"}]}}`)),
			}, nil
		}),
	}

	ack, err := uploadReportWithClient(client, "https://example.test", "eur_test", ReportPayload{
		Source:        "codex",
		ClientVersion: "0.142.2",
		Events: []UsageEvent{
			{EventID: "evt-1", TotalTokens: 10},
		},
	})
	if err != nil {
		t.Fatalf("UploadReport error: %v", err)
	}
	if len(ack.AcceptedEventIDs) != 1 || ack.AcceptedEventIDs[0] != "evt-1" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
}
