package usagereporter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// CodexCollectionPolicy identifies gateway routes locally. Provider names and
// endpoint configuration never become part of the uploaded usage payload.
type CodexCollectionPolicy struct {
	GatewayURLs       []string
	ExcludedProviders []string
	ConfigPath        string
}

type codexGatewayFilter struct {
	enabled   bool
	origins   map[string]string
	providers map[string]bool // true means already metered by the gateway
}

func loadCodexGatewayFilter(root, server string, policies []CodexCollectionPolicy) (*codexGatewayFilter, error) {
	gatewayURLs := make([]string, 0)
	filter := &codexGatewayFilter{providers: make(map[string]bool), origins: make(map[string]string)}
	urls := []string{server}
	configPath := filepath.Join(filepath.Dir(filepath.Clean(root)), "config.toml")
	for _, policy := range policies {
		urls = append(urls, policy.GatewayURLs...)
		if policy.ConfigPath != "" {
			configPath = policy.ConfigPath
		}
		for _, provider := range policy.ExcludedProviders {
			if provider = strings.TrimSpace(provider); provider != "" {
				filter.providers[provider] = true
				filter.enabled = true
			}
		}
	}
	for _, value := range urls {
		if strings.TrimSpace(value) == "" {
			continue
		}
		origin, err := gatewayOrigin(value)
		if err != nil {
			return nil, errors.New("invalid gateway URL in reporter configuration")
		}
		gatewayURLs = append(gatewayURLs, origin)
		filter.enabled = true
	}
	if !filter.enabled {
		return filter, nil
	}
	policy, err := NewRoutePolicy("", gatewayURLs, nil)
	if err != nil {
		return nil, err
	}

	// Decode only routing fields. Never retain keys, headers, or auth commands.
	var config struct {
		OpenAIBaseURL  string `toml:"openai_base_url"`
		ModelProviders map[string]struct {
			BaseURL string `toml:"base_url"`
		} `toml:"model_providers"`
	}
	content, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.New("could not read Codex provider configuration")
	}
	if err == nil {
		if err := toml.Unmarshal(content, &config); err != nil {
			// TOML diagnostics can contain source lines with credentials.
			return nil, errors.New("could not parse Codex provider configuration")
		}
	}
	for provider, route := range config.ModelProviders {
		if filter.providers[provider] || strings.TrimSpace(route.BaseURL) == "" {
			continue
		}
		origin, err := gatewayOrigin(route.BaseURL)
		if err != nil {
			return nil, errors.New("invalid base URL in Codex provider configuration")
		}
		allowed, err := policy.Allows(origin)
		if err != nil {
			return nil, err
		}
		filter.providers[provider] = !allowed
		filter.origins[provider] = origin
	}
	if !filter.providers["openai"] {
		baseURL := config.OpenAIBaseURL
		if baseURL == "" {
			baseURL = config.ModelProviders["openai"].BaseURL
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		origin, err := gatewayOrigin(baseURL)
		if err != nil {
			return nil, errors.New("invalid OpenAI base URL in Codex provider configuration")
		}
		allowed, err := policy.Allows(origin)
		if err != nil {
			return nil, err
		}
		filter.providers["openai"] = !allowed
		filter.origins["openai"] = origin
	}
	return filter, nil
}
