package usagereporter

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// loadZCodeProviderOrigins reads only routing fields from the current ZCode
// configuration. It deliberately does not retain or log keys or other fields.
func loadZCodeProviderOrigins(path string) (map[string]string, error) {
	path, err := ExpandPath(path)
	if err != nil {
		return nil, errors.New("invalid ZCode provider configuration path")
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, errors.New("could not read ZCode provider configuration")
	}
	defer f.Close()
	const maxConfigSize = 8 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxConfigSize+1))
	if err != nil || len(data) > maxConfigSize {
		return nil, errors.New("could not read ZCode provider configuration")
	}
	var config struct {
		Provider map[string]struct {
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
		} `json:"provider"`
	}
	if err := common.Unmarshal(data, &config); err != nil {
		return nil, errors.New("could not parse ZCode provider configuration")
	}
	origins := make(map[string]string, len(config.Provider))
	for id, provider := range config.Provider {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		origin, err := NormalizeUpstreamURL(provider.Options.BaseURL)
		if err != nil {
			origin = "" // Known provider with an invalid URL must remain unresolved.
		}
		if _, duplicate := origins[id]; duplicate {
			origin = ""
		}
		origins[id] = origin
	}
	return origins, nil
}

// loadZCodeEffectiveProviderOrigins combines the account-provider rules shipped
// with ZCode and user overrides with the current provider configuration.
// Duplicate, invalid, or conflicting origins are deliberately left unresolved.
func loadZCodeEffectiveProviderOrigins(currentPath, customPath string, builtinPaths []string) (map[string]string, error) {
	current, err := loadZCodeProviderOrigins(currentPath)
	if err != nil {
		return nil, err
	}
	builtins := map[string]string{}
	for _, path := range builtinPaths {
		rules, err := loadZCodeProviderRuleOrigins(path)
		if err != nil {
			return nil, err
		}
		for id, origin := range rules {
			if old, ok := builtins[id]; ok && old != origin {
				builtins[id] = ""
			} else if !ok {
				builtins[id] = origin
			}
		}
	}
	custom, err := loadZCodeProviderRuleOrigins(customPath)
	if err != nil {
		return nil, err
	}
	for id, origin := range custom {
		builtins[id] = origin
	}
	for id, origin := range current {
		builtins[id] = origin
	}
	return builtins, nil
}

func loadZCodeProviderRuleOrigins(path string) (map[string]string, error) {
	result := map[string]string{}
	if path == "" {
		return result, nil
	}
	path, err := ExpandPath(path)
	if err != nil {
		return nil, errors.New("invalid ZCode provider rule path")
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, errors.New("could not read ZCode provider rules")
	}
	defer f.Close()
	const maxConfigSize = 8 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxConfigSize+1))
	if err != nil || len(data) > maxConfigSize {
		return nil, errors.New("could not read ZCode provider rules")
	}
	var config struct {
		Config struct {
			ProviderConfigRules struct {
				ProviderRules []struct {
					ProviderID string `json:"providerId"`
					Config     struct {
						API struct {
							BaseURL string `json:"baseUrl"`
						} `json:"api"`
					} `json:"config"`
				} `json:"providerRules"`
			} `json:"providerConfigRules"`
		} `json:"config"`
	}
	if err := common.Unmarshal(data, &config); err != nil {
		return nil, errors.New("could not parse ZCode provider rules")
	}
	for _, rule := range config.Config.ProviderConfigRules.ProviderRules {
		id := strings.TrimSpace(rule.ProviderID)
		// Other provider kinds may be governed by templates or dynamic settings.
		if !strings.HasPrefix(id, "account:") {
			continue
		}
		origin, err := NormalizeUpstreamURL(rule.Config.API.BaseURL)
		if err != nil {
			origin = ""
		}
		if _, exists := result[id]; exists {
			origin = ""
		}
		result[id] = origin
	}
	return result, nil
}

func zcodeBuiltinProviderPaths(override string) []string {
	if override != "" {
		return []string{override}
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		paths := []string{"/Applications/ZCode.app/Contents/Resources/config/provider/zcode-builtin.json"}
		if home != "" {
			paths = append(paths, filepath.Join(home, "Applications/ZCode.app/Contents/Resources/config/provider/zcode-builtin.json"))
		}
		return paths
	case "windows":
		var paths []string
		if root := os.Getenv("LOCALAPPDATA"); root != "" {
			paths = append(paths, filepath.Join(root, "Programs", "ZCode", "resources", "config", "provider", "zcode-builtin.json"))
		}
		if root := os.Getenv("ProgramFiles"); root != "" {
			paths = append(paths, filepath.Join(root, "ZCode", "resources", "config", "provider", "zcode-builtin.json"))
		}
		return paths
	default:
		return nil
	}
}

func zcodeCurrentProviderOrigins(cfg Config) (map[string]string, error) {
	path, err := ExpandPath(cfg.ZCodeProviderConfigPath)
	if err != nil {
		return nil, errors.New("invalid ZCode provider configuration path")
	}
	return loadZCodeEffectiveProviderOrigins(path, filepath.Join(filepath.Dir(path), "provider_config.json"), zcodeBuiltinProviderPaths(cfg.ZCodeBuiltinProviderConfigPath))
}
