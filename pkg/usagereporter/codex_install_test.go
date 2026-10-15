package usagereporter

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexManualReinstallPreservesIdentityPathsFilterAndQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/external-usage/self/codex-installer/exchange", r.URL.Path)
		var payload map[string]string
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		assert.Equal(t, "existing-credential", payload["reporting_credential"])
		_, _ = w.Write([]byte(`{"success":true,"data":{"reporting_credential":"existing-credential","source":"codex","accept_window_days":90}}`))
	}))
	defer server.Close()
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	cfg := Config{Server: server.URL, Source: SourceCodex, Credential: "existing-credential", StatePath: filepath.Join(root, "custom-state.json"), CodexDir: filepath.Join(root, "custom-sessions"), GatewayURLs: []string{"https://gateway.example"}, ExcludedCodexProviders: []string{"gateway"}, CodexConfigPath: filepath.Join(root, "custom-codex.toml")}
	require.NoError(t, SaveConfig(path, cfg))
	reporter, err := NewReporter(ReporterConfig{Server: cfg.Server, Credential: cfg.Credential, Source: cfg.Source, StatePath: cfg.StatePath})
	require.NoError(t, err)
	queue := filepath.Join(reporter.queueDir, "pending.json")
	require.NoError(t, os.WriteFile(queue, []byte("queued usage"), 0o600))
	require.NoError(t, InstallCodexReporter(server.URL, "new-install-token", path))
	loaded, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, cfg.Credential, loaded.Credential)
	assert.Equal(t, cfg.StatePath, loaded.StatePath)
	assert.Equal(t, cfg.CodexDir, loaded.CodexDir)
	assert.Equal(t, cfg.GatewayURLs, loaded.GatewayURLs)
	assert.Equal(t, cfg.ExcludedCodexProviders, loaded.ExcludedCodexProviders)
	assert.Equal(t, cfg.CodexConfigPath, loaded.CodexConfigPath)
	assert.Equal(t, 90, loaded.AcceptWindowDays)
	data, err := os.ReadFile(queue)
	require.NoError(t, err)
	assert.Equal(t, "queued usage", string(data))
}
