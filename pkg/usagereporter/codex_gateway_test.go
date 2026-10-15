package usagereporter

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectUsageExcludesGatewayProviderWithSameModel(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(`[model_providers.gateway]
base_url = "https://token.example/v1"
[model_providers.direct]
base_url = "https://api.openai.com/v1"
`), 0o600))
	for _, provider := range []string{"gateway", "direct"} {
		content := `{"timestamp":"2026-07-15T00:00:00Z","type":"session_meta","payload":{"id":"` + provider + `","model_provider":"` + provider + `"}}
{"timestamp":"2026-07-15T00:00:01Z","type":"turn_context","payload":{"turn_id":"turn","model":"gpt-5"}}
{"timestamp":"2026-07-15T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}
`
		require.NoError(t, os.WriteFile(filepath.Join(sessions, provider+".jsonl"), []byte(content), 0o600))
	}
	state := NewState()
	state.Server = "https://token.example"
	result, err := CollectUsage(SourceCodex, sessions, state, 0)
	require.NoError(t, err)
	require.Len(t, result.Events, 1, "gateway usage already metered by the server must not be reported again")
	assert.Equal(t, sha256Hex("direct"), result.Events[0].SessionID)
	assert.Equal(t, 15, result.Events[0].TotalTokens)
}

// Codex token_count records contain cumulative totals rather than per-call usage.
func codexMeteringRecord(total int) string {
	return fmt.Sprintf(`{"timestamp":"2026-07-15T00:00:%02dZ","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"output_tokens":%d,"total_tokens":%d}}}}
`, total/15, total*2/3, total/3, total)
}

func codexMeteringSession(session, provider string) string {
	return fmt.Sprintf(`{"timestamp":"2026-07-15T00:00:00Z","type":"session_meta","payload":{"id":%q,"model_provider":%q}}
{"timestamp":"2026-07-15T00:00:00Z","type":"turn_context","payload":{"turn_id":"turn","model":"gpt-5"}}
`, session, provider) + codexMeteringRecord(15)
}

func TestGatewayFilterPreservesProviderAndCumulativeBaseline(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(`[model_providers.gateway]
base_url = "https://token.example/v1"
[model_providers.direct]
base_url = "https://api.openai.com/v1"
`), 0o600))
	path := filepath.Join(sessions, "session.jsonl")
	content := codexMeteringSession("mixed", "gateway")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	state := NewState()
	state.Server = "https://token.example"
	first, err := CollectUsage(SourceCodex, sessions, state, 0)
	require.NoError(t, err)
	assert.Empty(t, first.Events)
	assert.Equal(t, 1, first.ExcludedGatewayEvents)
	assert.Empty(t, state.SessionTotals, "collection alone cannot advance committed progress")
	statePath := filepath.Join(root, "state.json")
	require.NoError(t, first.NextState.Save(statePath))
	saved, err := LoadState(statePath)
	require.NoError(t, err)

	content += codexMeteringRecord(30)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	second, err := CollectUsage(SourceCodex, sessions, saved, 0)
	require.NoError(t, err)
	assert.Empty(t, second.Events)
	assert.Equal(t, 1, second.ExcludedGatewayEvents, "resumed scans retain provider identity")
	state = second.NextState
	content += `{"timestamp":"2026-07-15T00:00:03Z","type":"turn_context","payload":{"turn_id":"direct-turn","model":"gpt-5","model_provider":"direct"}}
` + codexMeteringRecord(45)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	third, err := CollectUsage(SourceCodex, sessions, state, 0)
	require.NoError(t, err)
	require.Len(t, third.Events, 1)
	assert.Equal(t, 15, third.Events[0].TotalTokens, "gateway totals must not leak into the following direct delta")
	retry, err := CollectUsage(SourceCodex, sessions, state, 0)
	require.NoError(t, err)
	assert.Equal(t, third.Events, retry.Events, "unacknowledged retries retain stable identities")
	ApplyAcknowledgements(state, third.NextState, &ReportAcknowledgement{AcceptedEventIDs: []string{third.Events[0].EventID}})
	fourth, err := CollectUsage(SourceCodex, sessions, state, 0)
	require.NoError(t, err)
	assert.Empty(t, fourth.Events)
}

func TestGatewayFilterDefersUnresolvedProviderUntilRouteIsKnown(t *testing.T) {
	for _, provider := range []string{"", "unresolved"} {
		t.Run("provider="+provider, func(t *testing.T) {
			root := t.TempDir()
			sessions := filepath.Join(root, "sessions")
			require.NoError(t, os.MkdirAll(sessions, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(sessions, "session.jsonl"), []byte(codexMeteringSession("unresolved-session", provider)), 0o600))
			state := NewState()
			state.Server = "https://token.example"
			first, err := CollectUsage(SourceCodex, sessions, state, 0)
			require.NoError(t, err)
			assert.Empty(t, first.Events)
			assert.Equal(t, 1, first.DeferredProviderEvents)
			state = first.NextState
			require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(`[model_providers.unresolved]
base_url = "https://api.other.example/v1"
`), 0o600))
			second, err := CollectUsage(SourceCodex, sessions, state, 0)
			require.NoError(t, err)
			if provider == "" {
				assert.Empty(t, second.Events, "current default provider must not be guessed for old sessions")
				assert.Equal(t, 1, second.DeferredProviderEvents)
				return
			}
			require.Len(t, second.Events, 1, "deferred metering is retried rather than discarded")
			assert.Equal(t, 15, second.Events[0].TotalTokens)
			assert.Zero(t, second.DeferredProviderEvents)
		})
	}
}

func TestGatewayFilterAliasesAndExplicitProviderExclusions(t *testing.T) {
	tests := []struct {
		name         string
		route        string
		gatewayURLs  []string
		excluded     []string
		wantExcluded bool
	}{
		{name: "canonical host and path", route: "https://TOKEN.example:443/v1/responses", wantExcluded: true},
		{name: "explicit internal alias", route: "http://172.16.0.37:3000/v1", gatewayURLs: []string{"http://172.16.0.37:3000"}, wantExcluded: true},
		{name: "different port", route: "https://token.example:8443/v1"},
		{name: "lookalike hostname", route: "https://token.example.attacker.test/v1"},
		{name: "another proxy", route: "https://proxy.other.example/v1"},
		{name: "explicit historical provider", route: "https://api.other.example/v1", excluded: []string{"custom"}, wantExcluded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			sessions := filepath.Join(root, "sessions")
			require.NoError(t, os.MkdirAll(sessions, 0o700))
			config := fmt.Sprintf("[model_providers.custom]\nbase_url = %q\n", tt.route)
			require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(sessions, "session.jsonl"), []byte(codexMeteringSession("session", "custom")), 0o600))
			state := NewState()
			state.Server = "https://token.example"
			result, err := CollectUsage(SourceCodex, sessions, state, 0, CodexCollectionPolicy{GatewayURLs: tt.gatewayURLs, ExcludedProviders: tt.excluded})
			require.NoError(t, err)
			if tt.wantExcluded {
				assert.Empty(t, result.Events)
				assert.Equal(t, 1, result.ExcludedGatewayEvents)
			} else {
				require.Len(t, result.Events, 1)
				assert.Equal(t, 15, result.Events[0].TotalTokens)
			}
		})
	}
}

func TestGatewayFilterUpgradesLegacyCheckpointWithoutRecounting(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	config := `[model_providers.gateway]
base_url = "https://token.example/v1"
[model_providers.direct]
base_url = "https://api.openai.com/v1"
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0o600))
	for _, provider := range []string{"gateway", "direct"} {
		require.NoError(t, os.WriteFile(filepath.Join(sessions, provider+".jsonl"), []byte(codexMeteringSession(provider, provider)), 0o600))
	}
	state := NewState()
	before, err := CollectUsage(SourceCodex, sessions, state, 0)
	require.NoError(t, err)
	require.Len(t, before.Events, 2)
	ids := []string{before.Events[0].EventID, before.Events[1].EventID}
	ApplyAcknowledgements(state, before.NextState, &ReportAcknowledgement{AcceptedEventIDs: ids})
	state.SessionContextVersion = 1
	state.Server = "https://token.example"
	for path, progress := range state.Files {
		progress.ModelProvider = ""
		state.Files[path] = progress
	}
	for _, provider := range []string{"gateway", "direct"} {
		content := codexMeteringSession(provider, provider) + codexMeteringRecord(30)
		require.NoError(t, os.WriteFile(filepath.Join(sessions, provider+".jsonl"), []byte(content), 0o600))
	}
	after, err := CollectUsage(SourceCodex, sessions, state, 0)
	require.NoError(t, err)
	require.Len(t, after.Events, 1)
	assert.Equal(t, sha256Hex("direct"), after.Events[0].SessionID)
	assert.Equal(t, 15, after.Events[0].TotalTokens)
	assert.NotContains(t, ids, after.Events[0].EventID)
	assert.Equal(t, 2, after.ExcludedGatewayEvents)
	assert.Len(t, state.Acknowledged, len(ids))
	for _, id := range ids {
		assert.Contains(t, state.Acknowledged, id)
	}
}

func TestGatewayFilterRejectsInvalidConfigurationWithoutLeakingSecrets(t *testing.T) {
	for _, content := range []string{
		"[model_providers.custom]\nbase_url = \"credential-secret",
		"[model_providers.custom]\nbase_url = \"ftp://credential-secret\"",
	} {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(content), 0o600))
		state := NewState()
		state.Server = "https://token.example"
		_, err := CollectUsage(SourceCodex, filepath.Join(root, "sessions"), state, 0)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "credential-secret")
		assert.Empty(t, state.Files)
	}
}

func TestReporterConfigRetainsGatewayPolicyAcrossSaveAndReload(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	initial := Config{Server: "https://token.example", Credential: "fixture-credential", GatewayURLs: []string{"http://172.16.0.37:3000"}, ExcludedCodexProviders: []string{"old-gateway"}, CodexConfigPath: "routing/config.toml"}
	require.NoError(t, SaveConfig(path, initial))
	require.NoError(t, SaveConfig(path, Config{Server: initial.Server, Credential: "rotated-fixture-credential"}))
	loaded, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, initial.GatewayURLs, loaded.GatewayURLs)
	assert.Equal(t, initial.ExcludedCodexProviders, loaded.ExcludedCodexProviders)
	assert.Equal(t, filepath.Join(root, initial.CodexConfigPath), loaded.CodexConfigPath)
}

func TestGatewayFilterResolvesBuiltinOpenAIRoute(t *testing.T) {
	tests := []struct {
		name         string
		config       string
		wantExcluded bool
	}{
		{name: "official default"},
		{name: "base URL override", config: `openai_base_url = "https://token.example/v1"`, wantExcluded: true},
		{name: "legacy provider definition", config: `[model_providers.openai]
base_url = "https://token.example/v1"`, wantExcluded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			sessions := filepath.Join(root, "relocated", "sessions")
			require.NoError(t, os.MkdirAll(sessions, 0o700))
			configPath := filepath.Join(root, "routing.toml")
			require.NoError(t, os.WriteFile(configPath, []byte(tt.config), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(sessions, "session.jsonl"), []byte(codexMeteringSession("builtin-session", "openai")), 0o600))
			result, err := CollectUsage(SourceCodex, sessions, NewState(), 0, CodexCollectionPolicy{GatewayURLs: []string{"https://token.example"}, ConfigPath: configPath})
			require.NoError(t, err)
			if tt.wantExcluded {
				assert.Empty(t, result.Events)
				assert.Equal(t, 1, result.ExcludedGatewayEvents)
			} else {
				require.Len(t, result.Events, 1)
				assert.Equal(t, 15, result.Events[0].TotalTokens)
			}
		})
	}
}
