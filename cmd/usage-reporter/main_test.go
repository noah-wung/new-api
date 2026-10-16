package main

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandReporterPathSupportsWindowsHomeSeparator(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	for _, path := range []string{"~/settings.json", `~\settings.json`} {
		actual, err := usagereporter.ExpandPath(path)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(home, "settings.json"), actual)
	}
}

func TestReporterCommandConfiguresClaudeUserSettingsWithoutUpload(t *testing.T) {
	for _, name := range []string{"ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		t.Setenv(name, "")
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	configPath := filepath.Join(root, "config.json")
	cfg := usagereporter.Config{Server: server.URL, Source: usagereporter.SourceClaudeCode, Credential: "private-reporting-credential", StatePath: filepath.Join(root, "state.json")}
	require.NoError(t, usagereporter.SaveConfig(configPath, cfg))
	output := runReporterCommand(t, "--config", configPath, "--configure-claude")
	assert.Contains(t, output, "restart Claude Code CLI and VS Code sessions")
	assert.Zero(t, requests.Load())
	loaded, err := usagereporter.LoadConfig(configPath)
	require.NoError(t, err)
	assert.Len(t, loaded.ClaudeReceiverToken, 64)
	settings, err := os.ReadFile(filepath.Join(root, "claude", "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(settings), "--claude-otel-headers")
	assert.True(t, loaded.ClaudeAutoRoute)
	assert.NotContains(t, string(settings), cfg.Credential)
	assert.NotContains(t, output, loaded.ClaudeReceiverToken)
	assert.NotContains(t, output, cfg.Credential)
}

// Exercise main in a child process so exits and global CLI flags behave as in
// an installed reporter, without re-entering the parent test runner.
func TestUsageReporterSubprocess(t *testing.T) {
	if os.Getenv("NEW_API_USAGE_REPORTER_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"usage-reporter"}, os.Args[i+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet("usage-reporter", flag.ExitOnError)
	main()
	os.Exit(0)
}

func runReporterCommand(t *testing.T, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-test.run=^TestUsageReporterSubprocess$", "--"}, args...)
	cmd := exec.Command(os.Args[0], commandArgs...)
	cmd.Env = append(os.Environ(), "NEW_API_USAGE_REPORTER_TEST_PROCESS=1", "HOME="+t.TempDir())
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return string(output)
}

func TestReporterCommandPersistsGatewayOnlyProgressWithoutUploading(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/external-usage/report" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte("[model_providers.gateway]\nbase_url = \""+server.URL+"/v1\"\n"), 0o600))
	content := `{"timestamp":"2026-07-15T00:00:00Z","type":"session_meta","payload":{"id":"gateway-session","model_provider":"gateway"}}
{"timestamp":"2026-07-15T00:00:01Z","type":"turn_context","payload":{"turn_id":"one","model":"gpt-5"}}
{"timestamp":"2026-07-15T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}
`
	path := filepath.Join(sessions, "session.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	statePath := filepath.Join(root, "state.json")
	args := []string{"--server", server.URL, "--credential", "fixture-credential", "--codex-dir", sessions, "--state", statePath}
	first := runReporterCommand(t, args...)
	assert.Contains(t, first, "excluded 1 gateway-metered usage events")
	assert.FileExists(t, statePath)
	second := runReporterCommand(t, args...)
	assert.NotContains(t, second, "excluded")
	content += `{"timestamp":"2026-07-15T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30}}}}
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	third := runReporterCommand(t, args...)
	assert.Contains(t, third, "excluded 1 gateway-metered usage events")
	assert.Zero(t, requests.Load(), "locally excluded usage needs no report or ACK request")
}

func TestReporterCommandPreservesPolicyDuringReinstall(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "config.json")
	pendingPath := filepath.Join(root, "pending", "config.json")
	old := usagereporter.Config{Server: "https://token.example", Credential: "old-fixture-credential", GatewayURLs: []string{"http://172.16.0.37:3000"}, ExcludedCodexProviders: []string{"historical-gateway"}, CodexConfigPath: "routing/config.toml"}
	require.NoError(t, usagereporter.SaveConfig(oldPath, old))
	require.NoError(t, usagereporter.SaveConfig(pendingPath, usagereporter.Config{Server: old.Server, Credential: "new-fixture-credential", AcceptWindowDays: 130}))
	output := runReporterCommand(t, "--config", pendingPath, "--preserve-gateway-policy-from", oldPath)
	assert.Empty(t, output, "policy preparation must not trigger an extra upload")
	actual, err := usagereporter.LoadConfig(pendingPath)
	require.NoError(t, err)
	assert.Equal(t, "new-fixture-credential", actual.Credential)
	assert.Equal(t, old.GatewayURLs, actual.GatewayURLs)
	assert.Equal(t, old.ExcludedCodexProviders, actual.ExcludedCodexProviders)
	assert.Equal(t, filepath.Join(root, old.CodexConfigPath), actual.CodexConfigPath)
	original, err := usagereporter.LoadConfig(oldPath)
	require.NoError(t, err)
	assert.Equal(t, "old-fixture-credential", original.Credential)
}

func TestReporterCommandRetriesFailedUploadWithoutRecountingGatewayUsage(t *testing.T) {
	received := make(chan usagereporter.ReportPayload, 4)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/external-usage/report" {
			http.NotFound(w, r)
			return
		}
		var payload usagereporter.ReportPayload
		if err := common.DecodeJson(r.Body, &payload); err != nil {
			t.Errorf("decode report: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		ids := make([]string, 0, len(payload.Events))
		for _, event := range payload.Events {
			ids = append(ids, event.EventID)
		}
		body, err := common.Marshal(map[string]any{"success": true, "data": usagereporter.ReportAcknowledgement{AcceptedEventIDs: ids}})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(body)
		assert.NoError(t, err)
	}))
	defer server.Close()
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	config := "[model_providers.gateway]\nbase_url = \"" + server.URL + "/v1\"\n[model_providers.direct]\nbase_url = \"https://api.openai.com/v1\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0o600))
	for _, provider := range []string{"gateway", "direct"} {
		content := `{"timestamp":"2026-07-15T00:00:00Z","type":"session_meta","payload":{"id":"` + provider + `","model_provider":"` + provider + `"}}
{"timestamp":"2026-07-15T00:00:01Z","type":"turn_context","payload":{"turn_id":"one","model":"gpt-5"}}
{"timestamp":"2026-07-15T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}
`
		require.NoError(t, os.WriteFile(filepath.Join(sessions, provider+".jsonl"), []byte(content), 0o600))
	}
	statePath := filepath.Join(root, "state.json")
	args := []string{"--server", server.URL, "--credential", "fixture-credential", "--codex-dir", sessions, "--state", statePath}
	commandArgs := append([]string{"-test.run=^TestUsageReporterSubprocess$", "--"}, args...)
	failed := exec.Command(os.Args[0], commandArgs...)
	failed.Env = append(os.Environ(), "NEW_API_USAGE_REPORTER_TEST_PROCESS=1", "HOME="+t.TempDir())
	output, err := failed.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "503")
	assert.NoFileExists(t, statePath, "failed uploads cannot acknowledge local progress")
	retryOutput := runReporterCommand(t, args...)
	require.Contains(t, retryOutput, "reported 1 events (1 accepted, 0 duplicate, 0 rejected)", "the next run must retry immediately after transport failure")
	require.Equal(t, int32(2), requests.Load())
	first, second := <-received, <-received
	require.Len(t, first.Events, 1)
	require.Len(t, second.Events, 1)
	assert.Equal(t, first.Events, second.Events)
	assert.Equal(t, 15, second.Events[0].TotalTokens)
	assert.Equal(t, "codex", second.Source)
	require.FileExists(t, statePath)
	finalOutput := runReporterCommand(t, args...)
	assert.Contains(t, finalOutput, "no new usage events")
	assert.Equal(t, int32(2), requests.Load(), "acknowledged direct usage and excluded gateway usage must not be uploaded again")
}

func TestReporterCommandWritesBackgroundDiagnosticsToLog(t *testing.T) {
	root := filepath.Join(t.TempDir(), "测试 logs")
	require.NoError(t, os.MkdirAll(root, 0o700))
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	logPath := filepath.Join(root, "scheduled-task.log")
	require.NoError(t, os.WriteFile(logPath, []byte("previous run\n"), 0o600))
	output := runReporterCommand(t, "--log-file", logPath, "--server", "https://token.example", "--credential", "fixture-credential", "--codex-dir", sessions, "--state", filepath.Join(root, "state.json"))
	assert.Empty(t, output, "successful background runs should write only to the log")
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "previous run\n")
	assert.Contains(t, string(content), "no new usage events")

	args := []string{"-test.run=^TestUsageReporterSubprocess$", "--", "--log-file", logPath, "--config", filepath.Join(root, "missing-config.json")}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "NEW_API_USAGE_REPORTER_TEST_PROCESS=1", "HOME="+t.TempDir())
	_, err = cmd.CombinedOutput()
	require.Error(t, err, "background errors must retain a nonzero exit code for Task Scheduler")
	content, err = os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "missing-config.json", "errors must remain diagnosable without a console")
	assert.Contains(t, string(content), "previous run\n")
	assert.Contains(t, string(content), "no new usage events")
	assert.NotContains(t, string(content), "fixture-credential")
}

func TestCodexReporterRetriesIncompleteACKFromSharedQueue(t *testing.T) {
	var uploads []usagereporter.ReportPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/external-usage/report" {
			http.NotFound(w, r)
			return
		}
		var payload usagereporter.ReportPayload
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		uploads = append(uploads, payload)
		ack := usagereporter.ReportAcknowledgement{}
		if len(uploads) > 1 {
			ack.AcceptedEventIDs = []string{payload.Events[0].EventID}
		}
		body, err := common.Marshal(map[string]any{"success": true, "data": ack})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	content := `{"timestamp":"2026-07-15T00:00:00Z","type":"session_meta","payload":{"id":"direct","model_provider":"openai"}}
{"timestamp":"2026-07-15T00:00:01Z","type":"turn_context","payload":{"turn_id":"turn","model":"gpt-5"}}
{"timestamp":"2026-07-15T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}
`
	require.NoError(t, os.WriteFile(filepath.Join(sessions, "session.jsonl"), []byte(content), 0o600))
	args := []string{"--server", server.URL, "--credential", "fixture", "--codex-dir", sessions, "--state", filepath.Join(root, "state.json")}
	first := runReporterCommand(t, args...)
	assert.Contains(t, first, "reported 1 events (0 accepted, 0 duplicate, 0 rejected)")
	second := runReporterCommand(t, args...)
	assert.Contains(t, second, "reported 1 events (1 accepted, 0 duplicate, 0 rejected)")
	require.Len(t, uploads, 2)
	assert.Equal(t, uploads[0].Events, uploads[1].Events, "scan progress cannot discard events missing from a server ACK")
	third := runReporterCommand(t, args...)
	assert.Contains(t, third, "no new usage events")
	assert.Len(t, uploads, 2)
}
