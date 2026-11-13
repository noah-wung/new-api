package usagereporter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZCodeProviderConfigOnlyUsesValidCurrentURLs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"provider": {
		"external": {"options": {"baseURL":"https://external.example/v1", "apiKey":"private-key"}},
		"gateway": {"options": {"baseURL":"http://TOKEN.EXAMPLE:8080/v1"}},
		"empty": {"options": {}},
		"credentials": {"options": {"baseURL":"https://user:secret@external.example/v1"}},
		"query": {"options": {"baseURL":"https://external.example/v1?key=secret"}}
	}}`), 0600))
	current, err := loadZCodeProviderOrigins(path)
	require.NoError(t, err)
	require.Equal(t, "https://external.example", current["external"])
	require.Equal(t, "http://token.example:8080", current["gateway"])
	for _, key := range []string{"empty", "credentials", "query"} {
		require.Contains(t, current, key)
		require.Empty(t, current[key])
	}
	policy, err := zcodeRoutePolicy(Config{Server: "https://token.example"})
	require.NoError(t, err)
	routes := []ZCodeProviderRoute{{ProviderID: "gateway", Origin: "https://external.example", From: 1, Until: 300}, {ProviderID: "empty", Origin: "https://external.example", From: 1, Until: 300}, {ProviderID: "legacy", Origin: "https://external.example", From: 1, Until: 300}}
	_, allowed, resolved, err := resolveZCodeCurrentRoute(policy, current, routes, nil, "gateway", 100, 101)
	require.NoError(t, err)
	require.True(t, resolved)
	require.False(t, allowed, "the current gateway overrides an old direct route on another port")
	_, _, resolved, err = resolveZCodeCurrentRoute(policy, current, routes, nil, "empty", 100, 101)
	require.NoError(t, err)
	require.False(t, resolved, "an invalid current URL must not use an older route")
	_, allowed, resolved, err = resolveZCodeCurrentRoute(policy, current, routes, nil, "legacy", 100, 101)
	require.NoError(t, err)
	require.True(t, resolved && allowed, "a missing provider can use explicitly configured legacy evidence")
	_, allowed, resolved, err = resolveZCodeCurrentRoute(policy, current, routes, []string{"external"}, "external", 100, 101)
	require.NoError(t, err)
	require.True(t, resolved && allowed, "a current direct URL overrides a historical provider exclusion")
	_, allowed, resolved, err = resolveZCodeCurrentRoute(policy, current, routes, []string{"legacy"}, "legacy", 100, 101)
	require.NoError(t, err)
	require.True(t, resolved && !allowed, "a legacy exclusion applies when the provider is absent from current configuration")
	_, _, resolved, err = resolveZCodeCurrentRoute(policy, current, routes, nil, "unknown", 100, 101)
	require.NoError(t, err)
	require.False(t, resolved)
}

func TestZCodeProviderConfigErrorsDoNotExposeContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	providers, err := loadZCodeProviderOrigins(path)
	require.NoError(t, err)
	require.Empty(t, providers)
	require.NoError(t, os.WriteFile(path, []byte(`{"provider":{"secret-value":`), 0600))
	_, err = loadZCodeProviderOrigins(path)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-value")
}

func TestZCodeAccountRoutesFromEffectiveProviderRules(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "config.json")
	custom := filepath.Join(root, "provider_config.json")
	builtin := filepath.Join(root, "zcode-builtin.json")
	require.NoError(t, os.WriteFile(current, []byte(`{"provider":{"direct":{"options":{"baseURL":"https://outside.example"}},"current-wins":{"options":{"baseURL":"https://token.example"}}}}`), 0600))
	require.NoError(t, os.WriteFile(builtin, []byte(`{"config":{"providerConfigRules":{"providerRules":[{"providerId":"account:direct","config":{"api":{"baseUrl":"https://official.example/v1"}}},{"providerId":"account:override","config":{"api":{"baseUrl":"https://official.example"}}},{"providerId":"current-wins","config":{"api":{"baseUrl":"https://official.example"}}},{"providerId":"account:ambiguous","config":{"api":{"baseUrl":"https://official.example"}}}]}}}`), 0600))
	require.NoError(t, os.WriteFile(custom, []byte(`{"config":{"providerConfigRules":{"providerRules":[{"providerId":"account:override","config":{"api":{"baseUrl":"https://token.example/v1"}}},{"providerId":"account:ambiguous","config":{"api":{}}}]}}}`), 0600))
	origins, err := loadZCodeEffectiveProviderOrigins(current, custom, []string{builtin})
	require.NoError(t, err)
	policy, err := zcodeRoutePolicy(Config{Server: "https://token.example"})
	require.NoError(t, err)
	for _, tc := range []struct {
		id                string
		allowed, resolved bool
	}{
		{"account:direct", true, true}, {"account:override", false, true},
		{"current-wins", false, true}, {"account:ambiguous", false, false}, {"missing", false, false},
	} {
		_, allowed, resolved, err := resolveZCodeCurrentRoute(policy, origins, nil, nil, tc.id, 100, 101)
		require.NoError(t, err, tc.id)
		require.Equal(t, tc.allowed, allowed, tc.id)
		require.Equal(t, tc.resolved, resolved, tc.id)
	}
}

func TestZCodeAccountRulesFailClosedOnConflictOrMalformedInput(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first.json"), filepath.Join(root, "second.json")
	require.NoError(t, os.WriteFile(first, []byte(`{"config":{"providerConfigRules":{"providerRules":[{"providerId":"account:duplicate","config":{"api":{"baseUrl":"https://one.example"}}},{"providerId":"account:duplicate","config":{"api":{"baseUrl":"https://one.example"}}},{"providerId":"account:conflict","config":{"api":{"baseUrl":"https://one.example"}}},{"providerId":"account:bad","config":{"api":{"baseUrl":"https://user:secret@one.example"}}}]}}}`), 0600))
	require.NoError(t, os.WriteFile(second, []byte(`{"config":{"providerConfigRules":{"providerRules":[{"providerId":"account:conflict","config":{"api":{"baseUrl":"https://two.example"}}}]}}}`), 0600))
	origins, err := loadZCodeEffectiveProviderOrigins(filepath.Join(root, "missing"), "", []string{first, second})
	require.NoError(t, err)
	for _, id := range []string{"account:duplicate", "account:conflict", "account:bad"} {
		require.Contains(t, origins, id)
		require.Empty(t, origins[id], id)
	}
	require.NoError(t, os.WriteFile(second, []byte(`{"secret-value":`), 0600))
	_, err = loadZCodeEffectiveProviderOrigins(filepath.Join(root, "missing"), "", []string{first, second})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-value")
}
