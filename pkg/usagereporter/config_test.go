package usagereporter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_PreservesExistingStatePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, Config{
		Server:     "https://example.test",
		Source:     SourceCodex,
		Credential: "cred-1",
		CodexDir:   "~/codex",
		StatePath:  "~/preserved-state.json",
	}); err != nil {
		t.Fatalf("SaveConfig initial error: %v", err)
	}

	if err := SaveConfig(path, Config{
		Server:     "https://example.test",
		Source:     SourceCodex,
		Credential: "cred-2",
		CodexDir:   "~/codex-next",
	}); err != nil {
		t.Fatalf("SaveConfig update error: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.StatePath != "~/preserved-state.json" {
		t.Fatalf("cfg.StatePath = %q, want %q", cfg.StatePath, "~/preserved-state.json")
	}
	if cfg.Credential != "cred-2" {
		t.Fatalf("cfg.Credential = %q, want %q", cfg.Credential, "cred-2")
	}
	if cfg.CodexDir != "~/codex-next" {
		t.Fatalf("cfg.CodexDir = %q, want %q", cfg.CodexDir, "~/codex-next")
	}
}

func TestLoadConfig_AppliesDefaultsAndTrimsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"server":" https://example.test/ ","credential":" cred-1 "}`), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Server != "https://example.test/" {
		t.Fatalf("cfg.Server = %q, want %q", cfg.Server, "https://example.test/")
	}
	if cfg.Credential != "cred-1" {
		t.Fatalf("cfg.Credential = %q, want %q", cfg.Credential, "cred-1")
	}
	if cfg.Source != SourceCodex {
		t.Fatalf("cfg.Source = %q, want %q", cfg.Source, SourceCodex)
	}
	if cfg.CodexDir != "~/.codex/sessions" {
		t.Fatalf("cfg.CodexDir = %q, want %q", cfg.CodexDir, "~/.codex/sessions")
	}
	if cfg.StatePath != "~/.new-api-usage-reporter/state.json" {
		t.Fatalf("cfg.StatePath = %q, want %q", cfg.StatePath, "~/.new-api-usage-reporter/state.json")
	}
	if cfg.AcceptWindowDays != 0 {
		t.Fatalf("cfg.AcceptWindowDays = %d, want 0", cfg.AcceptWindowDays)
	}
}

func TestLoadConfig_PreservesAcceptWindowDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
		"server":"https://example.test",
		"credential":"cred-1",
		"accept_window_days":90
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.AcceptWindowDays != 90 {
		t.Fatalf("cfg.AcceptWindowDays = %d, want 90", cfg.AcceptWindowDays)
	}
}

func TestLoadConfig_RejectsMissingRequiredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"source":"codex"}`), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("LoadConfig error = nil, want non-nil")
	}
}

func TestLoadConfig_ResolvesRelativePathsFromConfigDirectory(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "reporter")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	path := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(path, []byte(`{
		"server":"https://example.test",
		"credential":"cred-1",
		"codex_dir":"sessions",
		"state_path":"state/state.json"
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.CodexDir != filepath.Join(configDir, "sessions") {
		t.Fatalf("cfg.CodexDir = %q, want %q", cfg.CodexDir, filepath.Join(configDir, "sessions"))
	}
	if cfg.StatePath != filepath.Join(configDir, "state", "state.json") {
		t.Fatalf("cfg.StatePath = %q, want %q", cfg.StatePath, filepath.Join(configDir, "state", "state.json"))
	}
}

func TestApplyExplicitOverrides_OverridesLoadedConfig(t *testing.T) {
	base := Config{
		Server:     "https://config.test",
		Source:     SourceCodex,
		Credential: "cfg-cred",
		CodexDir:   "/config/codex",
		StatePath:  "/config/state.json",
	}
	override := Config{
		Server:     "https://flag.test",
		Source:     "minimax_code",
		Credential: "flag-cred",
		CodexDir:   "/flag/codex",
		StatePath:  "/flag/state.json",
	}

	cfg := ApplyExplicitOverrides(base, override, map[string]bool{
		"server":     true,
		"credential": true,
		"codex-dir":  true,
		"state":      true,
	})
	if cfg.Server != "https://flag.test" {
		t.Fatalf("cfg.Server = %q, want %q", cfg.Server, "https://flag.test")
	}
	if cfg.Credential != "flag-cred" {
		t.Fatalf("cfg.Credential = %q, want %q", cfg.Credential, "flag-cred")
	}
	if cfg.CodexDir != "/flag/codex" {
		t.Fatalf("cfg.CodexDir = %q, want %q", cfg.CodexDir, "/flag/codex")
	}
	if cfg.StatePath != "/flag/state.json" {
		t.Fatalf("cfg.StatePath = %q, want %q", cfg.StatePath, "/flag/state.json")
	}
	if cfg.Source != SourceCodex {
		t.Fatalf("cfg.Source = %q, want %q", cfg.Source, SourceCodex)
	}
}
