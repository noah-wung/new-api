package usagereporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	DefaultCodexDir  = "~/.codex/sessions"
	DefaultStatePath = "~/.new-api-usage-reporter/state.json"
)

type Config struct {
	Server           string `json:"server"`
	Source           string `json:"source"`
	Credential       string `json:"credential"`
	CodexDir         string `json:"codex_dir"`
	StatePath        string `json:"state_path"`
	AcceptWindowDays int    `json:"accept_window_days,omitempty"`
}

func LoadConfig(path string) (*Config, error) {
	cfg, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	cfg = cfg.withDefaults()
	cfg = cfg.resolveRelativePaths(filepath.Dir(path))
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(path string, cfg Config) error {
	cfg = cfg.trimmed()
	existing, err := readConfig(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if cfg.StatePath == "" {
			cfg.StatePath = existing.StatePath
		}
		if cfg.AcceptWindowDays == 0 {
			cfg.AcceptWindowDays = existing.AcceptWindowDays
		}
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content, err := common.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func (c Config) WithDefaults() Config {
	return c.withDefaults()
}

func (c Config) Validate() error {
	if serverIdentity(c.Server) == "" {
		return fmt.Errorf("server is required")
	}
	if strings.TrimSpace(c.Credential) == "" {
		return fmt.Errorf("credential is required")
	}
	return nil
}

func ApplyExplicitOverrides(base Config, override Config, explicitlySet map[string]bool) Config {
	base = base.withDefaults()
	override = override.withDefaults()
	if explicitlySet["server"] {
		base.Server = override.Server
	}
	if explicitlySet["credential"] {
		base.Credential = override.Credential
	}
	if explicitlySet["source"] {
		base.Source = override.Source
	}
	if explicitlySet["codex-dir"] {
		base.CodexDir = override.CodexDir
	}
	if explicitlySet["state"] {
		base.StatePath = override.StatePath
	}
	return base.withDefaults()
}

func readConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{}
	if len(content) == 0 {
		return cfg, nil
	}
	if err := common.Unmarshal(content, &cfg); err != nil {
		return Config{}, err
	}
	return cfg.trimmed(), nil
}

func (c Config) withDefaults() Config {
	c = c.trimmed()
	if c.Source == "" {
		c.Source = SourceCodex
	}
	if c.CodexDir == "" {
		c.CodexDir = DefaultCodexDir
	}
	if c.StatePath == "" {
		c.StatePath = DefaultStatePath
	}
	return c
}

func (c Config) trimmed() Config {
	c.Server = strings.TrimSpace(c.Server)
	c.Source = strings.TrimSpace(c.Source)
	c.Credential = strings.TrimSpace(c.Credential)
	c.CodexDir = strings.TrimSpace(c.CodexDir)
	c.StatePath = strings.TrimSpace(c.StatePath)
	return c
}

func (c Config) resolveRelativePaths(baseDir string) Config {
	c.CodexDir = resolveRelativePath(baseDir, c.CodexDir)
	c.StatePath = resolveRelativePath(baseDir, c.StatePath)
	return c
}

func resolveRelativePath(baseDir string, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "~") || filepath.IsAbs(trimmed) {
		return trimmed
	}
	return filepath.Join(baseDir, trimmed)
}

func serverIdentity(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}
