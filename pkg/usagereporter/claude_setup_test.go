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
	binaryPath := filepath.Join(t.TempDir(), "reporter")
	for i := 0; i < 2; i++ {
		require.NoError(t, ConfigureClaudeTelemetry(cfg, settingsPath, binaryPath))
	}
	backup, err := os.ReadFile(settingsPath + ".before-new-api-telemetry")
	require.NoError(t, err)
	assert.Equal(t, previous, string(backup))
	content, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	var settings struct {
		Permissions map[string][]string `json:"permissions"`
		Helper      string              `json:"otelHeadersHelper"`
		Hooks       map[string]any      `json:"hooks"`
		Env         map[string]string   `json:"env"`
	}
	require.NoError(t, common.Unmarshal(content, &settings))
	assert.Equal(t, []string{"Bash(rm:*)"}, settings.Permissions["deny"])
	assert.Contains(t, settings.Hooks, "Stop")
	assert.Contains(t, settings.Helper, "--claude-otel-headers")
	assert.Contains(t, settings.Helper, binaryPath)
	assert.Equal(t, "private-fixture", settings.Env["ANTHROPIC_API_KEY"])
	assert.Equal(t, "console", settings.Env["OTEL_METRICS_EXPORTER"])
	assert.Equal(t, "0", settings.Env["OTEL_LOG_RAW_API_BODIES"])
	assert.Equal(t, "http/json", settings.Env["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"])
	assert.Equal(t, "http://127.0.0.1:4318/v1/logs", settings.Env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"])
	assert.Equal(t, "department=engineering", settings.Env["OTEL_RESOURCE_ATTRIBUTES"])
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

func TestConfigureClaudeTelemetryDoesNotRequireAnAddress(t *testing.T) {
	cfg := claudeReceiverConfig(t, "https://token.example")
	for _, settings := range []string{`{}`, `{"env":{"ANTHROPIC_BASE_URL":"https://third-party.example/v1"}}`, `{"env":{"ANTHROPIC_BASE_URL":"https://token.example"}}`, `{"env":{"CLAUDE_CODE_USE_BEDROCK":"1"}}`} {
		path := filepath.Join(t.TempDir(), "settings.json")
		require.NoError(t, os.WriteFile(path, []byte(settings), 0o600))
		require.NoError(t, ConfigureClaudeTelemetry(cfg, path, filepath.Join(t.TempDir(), "reporter")))
	}
}

func TestConfigureClaudeTelemetryPreservesIncompatibleSettings(t *testing.T) {
	cfg := claudeReceiverConfig(t, "https://token.example")
	for _, settings := range []string{`{"env":`, `{"env":null}`, `{"otelHeadersHelper":"my-company-helper"}`} {
		path := filepath.Join(t.TempDir(), "settings.json")
		require.NoError(t, os.WriteFile(path, []byte(settings), 0o600))
		require.Error(t, ConfigureClaudeTelemetry(cfg, path, filepath.Join(t.TempDir(), "reporter")))
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, settings, string(content))
	}
}

func TestClaudeManagedHelperMigrationDefersSettingsUntilCommit(t *testing.T) {
	cfg := claudeReceiverConfig(t, "https://token.example")
	root := t.TempDir()
	settings := filepath.Join(root, "settings.json")
	legacy := filepath.Join(root, "claude", "usage-reporter")
	stable := filepath.Join(root, "runtime", "launcher", "usage-reporter")
	old := `{"env":{"ANTHROPIC_BASE_URL":"https://third.example","OTEL_RESOURCE_ATTRIBUTES":"new_api.upstream_origin=https://old.example"},"otelHeadersHelper":`
	command, err := claudeTelemetryCommand(legacy, "")
	require.NoError(t, err)
	encoded, err := common.Marshal(command)
	require.NoError(t, err)
	old += string(encoded) + `}`
	require.NoError(t, os.WriteFile(settings, []byte(old), 0o600))
	options := ClaudeTelemetryOptions{RuntimeDir: filepath.Join(root, "runtime"), LegacyBinary: legacy, ValidateOnly: true}
	require.NoError(t, ConfigureClaudeTelemetry(cfg, settings, stable, options))
	before, err := os.ReadFile(settings)
	require.NoError(t, err)
	assert.Equal(t, old, string(before), "failed/rolled-back enrollment must leave legacy telemetry working")
	options.ValidateOnly = false
	require.NoError(t, ConfigureClaudeTelemetry(cfg, settings, stable, options))
	for count := 0; count < 2; count++ {
		require.NoError(t, ConfigureClaudeTelemetry(cfg, settings, stable, options))
	}
	content, err := os.ReadFile(settings)
	require.NoError(t, err)
	var result struct {
		Helper string            `json:"otelHeadersHelper"`
		Env    map[string]string `json:"env"`
	}
	require.NoError(t, common.Unmarshal(content, &result))
	assert.Contains(t, result.Helper, stable)
	assert.Contains(t, result.Helper, "--runtime")
	assert.Equal(t, "https://third.example", result.Env["ANTHROPIC_BASE_URL"])
	assert.NotContains(t, result.Env["OTEL_RESOURCE_ATTRIBUTES"], ClaudeUpstreamAttribute)
}
