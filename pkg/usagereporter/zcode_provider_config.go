package usagereporter

import (
	"errors"
	"io"
	"os"
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
