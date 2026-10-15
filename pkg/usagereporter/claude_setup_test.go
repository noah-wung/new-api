package usagereporter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigureClaudeTelemetryPreservesSettingsAndPolicy(t *testing.T) {
	for _, key := range []string{"ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		t.Setenv(key, "")
	}
	cfg := claudeReceiverConfig(t, "https://token.example")
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	previous := `{"permissions":{"deny":["Bash(rm:*)"]},"hooks":{"Stop":[]},"env":{"ANTHROPIC_API_KEY":"private-fixture","OTEL_RESOURCE_ATTRIBUTES":"department=engineering,new_api.upstream_origin=https://old.example","OTEL_METRICS_EXPORTER":"console","OTEL_LOG_RAW_API_BODIES":"1"}}`
	require.NoError(t, os.WriteFile(settingsPath, []byte(previous), 0o600))
	for i := 0; i < 2; i++ {
		require.NoError(t, ConfigureClaudeTelemetry(cfg, settingsPath, "https://api.anthropic.com"))
	}
	backup, err := os.ReadFile(settingsPath + ".before-new-api-telemetry")
	require.NoError(t, err)
	assert.Equal(t, previous, string(backup))
	content, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	var settings struct {
		Permissions map[string][]string `json:"permissions"`
		Hooks       map[string]any      `json:"hooks"`
		Env         map[string]string   `json:"env"`
	}
	require.NoError(t, common.Unmarshal(content, &settings))
	assert.Equal(t, []string{"Bash(rm:*)"}, settings.Permissions["deny"])
	assert.Contains(t, settings.Hooks, "Stop")
	assert.Equal(t, "private-fixture", settings.Env["ANTHROPIC_API_KEY"])
	assert.Equal(t, "console", settings.Env["OTEL_METRICS_EXPORTER"])
	assert.Equal(t, "0", settings.Env["OTEL_LOG_RAW_API_BODIES"])
	assert.Equal(t, "http/json", settings.Env["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"])
	assert.Equal(t, "http://127.0.0.1:4318/v1/logs", settings.Env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"])
	assert.Equal(t, "department=engineering,new_api.upstream_origin=https://api.anthropic.com", settings.Env["OTEL_RESOURCE_ATTRIBUTES"])
	assert.NotContains(t, settings.Env, "ANTHROPIC_BASE_URL", "telemetry configuration must not change API routing")

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg.ClaudeExternalURLs = []string{}
	require.NoError(t, SaveConfig(configPath, cfg))
	loaded, err := LoadConfig(configPath)
	require.NoError(t, err)
	require.NotNil(t, loaded.ClaudeExternalURLs, "explicit deny-all must survive saving")
	assert.Empty(t, loaded.ClaudeExternalURLs)
	loaded.AcceptWindowDays = 10
	require.NoError(t, SaveConfig(configPath, *loaded))
	loaded, err = LoadConfig(configPath)
	require.NoError(t, err)
	assert.Equal(t, cfg.ClaudeReceiverToken, loaded.ClaudeReceiverToken)
	assert.Equal(t, cfg.ClaudeListen, loaded.ClaudeListen)
}

func TestConfigureClaudeTelemetryRejectsAmbiguousRouting(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "")
	t.Setenv("CLAUDE_CODE_USE_VERTEX", "")
	t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "")
	cfg := claudeReceiverConfig(t, "https://token.example")
	for _, tc := range []struct{ name, settings, upstream string }{
		{"undeclared", `{}`, ""},
		{"custom not allowed", `{"env":{"ANTHROPIC_BASE_URL":"https://custom.example"}}`, "https://custom.example"},
		{"gateway mismatch", `{"env":{"ANTHROPIC_BASE_URL":"https://token.example"}}`, "https://api.anthropic.com"},
		{"alternate provider", `{"env":{"CLAUDE_CODE_USE_BEDROCK":"1"}}`, "https://api.anthropic.com"},
		{"broken settings", `{"env":`, "https://api.anthropic.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.settings), 0o600))
			require.Error(t, ConfigureClaudeTelemetry(cfg, path, tc.upstream))
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tc.settings, string(content))
		})
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://token.example/v1"}}`), 0o600))
	require.NoError(t, ConfigureClaudeTelemetry(cfg, path, "https://token.example"), "gateway traffic can be tagged explicitly for exclusion")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "new_api.upstream_origin=https://token.example")
}
