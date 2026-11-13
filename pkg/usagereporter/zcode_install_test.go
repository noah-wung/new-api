package usagereporter

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestInstallZCodeFirstTimeUsesRequestedServer(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	dbPath := filepath.Join(root, "usage.sqlite")
	t.Setenv("ZCODE_SESSION_DB_PATH", dbPath)
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.Equal(t, "/api/external-usage/self/zcode-installer/exchange", r.URL.Path)
		_, _ = w.Write([]byte(`{"success":true,"data":{"reporting_credential":"test-credential","source":"zcode"}}`))
	}))
	defer server.Close()
	path := filepath.Join(root, "zcode", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, InstallZCodeReporter(server.URL, "test-token", path))
	require.True(t, called)
	cfg, err := readConfig(path)
	require.NoError(t, err)
	require.Equal(t, server.URL, cfg.Server)
	require.Equal(t, SourceZCode, cfg.Source)
	require.Equal(t, "test-credential", cfg.Credential)
}

func TestInstallZCodePreservesExistingConfiguration(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "usage.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, common.DecodeJson(r.Body, &body))
		require.Equal(t, "old-credential", body["reporting_credential"])
		_, _ = w.Write([]byte(`{"success":true,"data":{"reporting_credential":"old-credential","source":"zcode"}}`))
	}))
	defer server.Close()
	path := filepath.Join(root, "config.json")
	previous := Config{Server: server.URL, Source: SourceZCode, Credential: "old-credential", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath, ZCodeProviderConfigPath: filepath.Join(root, "providers.json"), ZCodeGatewayHosts: []string{"gateway.example"}}
	require.NoError(t, SaveConfig(path, previous))
	previous, err = readConfig(path)
	require.NoError(t, err)
	require.NoError(t, InstallZCodeReporter(server.URL, "test-token", path))
	current, err := readConfig(path)
	require.NoError(t, err)
	require.Equal(t, previous, current)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.ErrorContains(t, InstallZCodeReporter("https://different.example", "test-token", path), "different identity")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestInstallZCodeRejectsUnreadableSourceBeforeExchange(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ZCODE_SESSION_DB_PATH", filepath.Join(root, "missing.sqlite"))
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	path := filepath.Join(root, "zcode", "config.json")
	require.ErrorContains(t, InstallZCodeReporter(server.URL, "test-token", path), "ZCode database is unavailable")
	require.False(t, called, "local preflight failure must not consume the install token")
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "failed installation must not create a config")
}

func TestInstallZCodeRejectsInvalidLocalDataBeforeExchange(t *testing.T) {
	for _, kind := range []string{"schema", "provider_config"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			dbPath := filepath.Join(root, "usage.sqlite")
			t.Setenv("ZCODE_SESSION_DB_PATH", dbPath)
			db, err := sql.Open("sqlite", dbPath)
			require.NoError(t, err)
			schema := `CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`
			if kind == "schema" {
				schema = `CREATE TABLE model_usage (id TEXT)`
			}
			_, err = db.Exec(schema)
			require.NoError(t, err)
			require.NoError(t, db.Close())
			if kind == "provider_config" {
				providerPath := filepath.Join(root, ".zcode", "v2", "config.json")
				require.NoError(t, os.MkdirAll(filepath.Dir(providerPath), 0o700))
				require.NoError(t, os.WriteFile(providerPath, []byte(`{invalid`), 0o600))
			}
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			defer server.Close()
			path := filepath.Join(root, "config.json")
			err = InstallZCodeReporter(server.URL, "test-token", path)
			if kind == "schema" {
				require.ErrorContains(t, err, "unsupported or unreadable ZCode usage schema")
			} else {
				require.ErrorContains(t, err, "could not parse ZCode provider configuration")
			}
			require.False(t, called, "invalid local data must not consume the install token")
			_, err = os.Stat(path)
			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestInstallZCodeExchangeFailurePreservesConfig(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "usage.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rejected", http.StatusForbidden)
	}))
	defer server.Close()
	path := filepath.Join(root, "config.json")
	require.NoError(t, SaveConfig(path, Config{Server: server.URL, Source: SourceZCode, Credential: "old-credential", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath}))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.ErrorContains(t, InstallZCodeReporter(server.URL, "expired-token", path), "existing identity was not replaced")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestInstallZCodeThenReportAndReinstall(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	dbPath := filepath.Join(root, "usage.sqlite")
	t.Setenv("ZCODE_SESSION_DB_PATH", dbPath)
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	for _, id := range []string{"direct", "gateway", "unknown"} {
		now := time.Now().UnixMilli()
		_, err = db.Exec(`INSERT INTO model_usage VALUES (?, 'session', ?, 'model', 'completed', ?, ?, 10, 5, 0, 0, 0, 15)`, id, id, now, now)
		require.NoError(t, err)
	}
	var reports []ReportPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/external-usage/self/zcode-installer/exchange":
			_, _ = w.Write([]byte(`{"success":true,"data":{"reporting_credential":"fixture","source":"zcode","accept_window_days":90}}`))
		case "/api/external-usage/report":
			var report ReportPayload
			if common.DecodeJson(r.Body, &report) != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			reports = append(reports, report)
			var ids []string
			for _, event := range report.Events {
				ids = append(ids, event.EventID)
			}
			body, _ := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	providerPath := filepath.Join(root, ".zcode", "v2", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(providerPath), 0o700))
	providerJSON, err := common.Marshal(map[string]any{"provider": map[string]any{"direct": map[string]any{"options": map[string]string{"baseURL": "https://external.example"}}, "gateway": map[string]any{"options": map[string]string{"baseURL": server.URL}}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(providerPath, providerJSON, 0o600))
	config := filepath.Join(root, ".new-api-usage-reporter", "zcode", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(config), 0o700))
	require.NoError(t, InstallZCodeReporter(server.URL, "fixture-token", config))
	cfg, err := LoadConfig(config)
	require.NoError(t, err)
	require.NoError(t, RunClient(context.Background(), *cfg, false, io.Discard, io.Discard))
	require.Len(t, reports, 1)
	require.Len(t, reports[0].Events, 1, "only the external provider should be reported")
	statePath, err := ExpandPath(cfg.StatePath)
	require.NoError(t, err)
	stateBefore, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.NoError(t, InstallZCodeReporter(server.URL, "another-token", config))
	stateAfter, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.Equal(t, stateBefore, stateAfter)
	cfg, err = LoadConfig(config)
	require.NoError(t, err)
	require.NoError(t, RunClient(context.Background(), *cfg, false, io.Discard, io.Discard))
	require.Len(t, reports, 1, "reinstallation must not resend acknowledged usage")
}
