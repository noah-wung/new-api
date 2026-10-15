package usagereporter

import (
	"errors"
	"net"
	"net/url"
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
	providers map[string]bool // true means already metered by the gateway
}

func loadCodexGatewayFilter(root, server string, policies []CodexCollectionPolicy) (*codexGatewayFilter, error) {
	origins := make(map[string]struct{})
	filter := &codexGatewayFilter{providers: make(map[string]bool)}
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
		origins[origin] = struct{}{}
		filter.enabled = true
	}
	if !filter.enabled {
		return filter, nil
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
		_, excluded := origins[origin]
		filter.providers[provider] = excluded
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
		_, excluded := origins[origin]
		filter.providers["openai"] = excluded
	}
	return filter, nil
}

func gatewayOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("expected an HTTP or HTTPS URL")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	port := parsed.Port()
	if port == "" || parsed.Scheme == "http" && port == "80" || parsed.Scheme == "https" && port == "443" {
		return parsed.Scheme + "://" + host, nil
	}
	return parsed.Scheme + "://" + net.JoinHostPort(host, port), nil
}
