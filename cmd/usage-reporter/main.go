package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/pkg/usagereporter"
)

func main() {
	if err := runReporter(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runReporter() (runErr error) {
	logPath := flag.String("log-file", "", "Append output and errors to this file for background execution")
	configPath := flag.String("config", "", "Reporter config file")
	preservePolicyFrom := flag.String("preserve-gateway-policy-from", "", "Copy gateway policy from an existing config into --config and exit")
	server := flag.String("server", "", "External usage API server URL")
	credential := flag.String("credential", "", "Dedicated reporting credential")
	source := flag.String("source", usagereporter.SourceCodex, "Usage source to report")
	codexDir := flag.String("codex-dir", "~/.codex/sessions", "Codex sessions directory")
	statePath := flag.String("state", "~/.new-api-usage-reporter/state.json", "Reporter state file")
	serve := flag.Bool("serve", false, "Run a streaming usage collector until interrupted")
	configureClaude := flag.Bool("configure-claude", false, "Merge telemetry into Claude user settings and exit")
	claudeSettings := flag.String("claude-settings", "", "Claude user settings.json (defaults to CLAUDE_CONFIG_DIR/settings.json or ~/.claude/settings.json)")
	claudeUpstream := flag.String("claude-upstream", "", "Explicit effective upstream URL for the Claude telemetry profile")
	installClaude := flag.Bool("install-claude", false, "Exchange a one-time Claude install token and configure telemetry")
	installToken := flag.String("install-token", "", "One-time Claude installer token")
	flag.Parse()
	if *logPath != "" {
		path, err := usagereporter.ExpandPath(*logPath)
		if err != nil {
			return err
		}
		logFile, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("open reporter log: %w", err)
		}
		stdout, stderr := os.Stdout, os.Stderr
		os.Stdout, os.Stderr = logFile, logFile
		defer func() {
			if runErr != nil {
				_, _ = fmt.Fprintln(logFile, runErr)
			}
			os.Stdout, os.Stderr = stdout, stderr
			_ = logFile.Close()
		}()
	}
	if *installClaude {
		if *configPath == "" || *server == "" || *installToken == "" {
			return errors.New("--install-claude requires --server, --install-token and --config")
		}
		path, err := usagereporter.ExpandPath(*configPath)
		if err != nil {
			return err
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return err
		}
		settings := *claudeSettings
		if settings == "" {
			dir := os.Getenv("CLAUDE_CONFIG_DIR")
			if dir == "" {
				dir = "~/.claude"
			}
			settings = filepath.Join(dir, "settings.json")
		}
		settings, err = usagereporter.ExpandPath(settings)
		if err != nil {
			return err
		}
		if err := usagereporter.InstallClaudeReporter(*server, *installToken, path, settings); err != nil {
			return err
		}
		fmt.Println("Claude reporter configured; restart Claude Code CLI and VS Code sessions")
		return nil
	}

	overrideConfig := usagereporter.Config{
		Server:     *server,
		Source:     *source,
		Credential: *credential,
		CodexDir:   *codexDir,
		StatePath:  *statePath,
	}.WithDefaults()
	expandedConfigPath := ""
	explicitFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})
	cfg := overrideConfig
	if trimmedConfigPath := strings.TrimSpace(*configPath); trimmedConfigPath != "" {
		var err error
		expandedConfigPath, err = usagereporter.ExpandPath(trimmedConfigPath)
		if err != nil {
			return err
		}
		loadedConfig, err := usagereporter.LoadConfig(expandedConfigPath)
		if err != nil {
			return err
		}
		cfg = usagereporter.ApplyExplicitOverrides(*loadedConfig, overrideConfig, explicitFlags)
	} else if err := cfg.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(*preservePolicyFrom) != "" {
		if expandedConfigPath == "" {
			return errors.New("--config is required when preserving gateway policy")
		}
		previousPath, err := usagereporter.ExpandPath(*preservePolicyFrom)
		if err != nil {
			return err
		}
		previous, err := usagereporter.LoadConfig(previousPath)
		if err != nil {
			return err
		}
		cfg.GatewayURLs = previous.GatewayURLs
		cfg.ExcludedCodexProviders = previous.ExcludedCodexProviders
		cfg.CodexConfigPath = previous.CodexConfigPath
		if err := usagereporter.SaveConfig(expandedConfigPath, cfg); err != nil {
			return err
		}
		return nil
	}
	if *configureClaude {
		if cfg.Source != usagereporter.SourceClaudeCode || expandedConfigPath == "" {
			return errors.New("--configure-claude requires --config with source claude_code")
		}
		if *serve {
			return errors.New("--configure-claude and --serve must be run separately")
		}
		if cfg.ClaudeReceiverToken == "" {
			var token [32]byte
			if _, err := rand.Read(token[:]); err != nil {
				return err
			}
			cfg.ClaudeReceiverToken = hex.EncodeToString(token[:])
			if err := usagereporter.SaveConfig(expandedConfigPath, cfg); err != nil {
				return err
			}
		}
		settingsPath := *claudeSettings
		if settingsPath == "" {
			settingsDir := os.Getenv("CLAUDE_CONFIG_DIR")
			if settingsDir == "" {
				settingsDir = "~/.claude"
			}
			settingsPath = filepath.Join(settingsDir, "settings.json")
		}
		settingsPath, err := usagereporter.ExpandPath(settingsPath)
		if err != nil {
			return err
		}
		if err := usagereporter.ConfigureClaudeTelemetry(cfg, settingsPath, *claudeUpstream); err != nil {
			return err
		}
		fmt.Println("Claude telemetry configured; restart Claude Code CLI and VS Code sessions")
		return nil
	}
	if expandedConfigPath != "" {
		client := &http.Client{Timeout: 15 * time.Second}
		changed, err := usagereporter.BackfillAcceptWindowDaysFromServer(client, &cfg)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: could not backfill accept_window_days: %v\n", err)
		} else if changed {
			if err := usagereporter.SaveConfig(expandedConfigPath, cfg); err != nil {
				return err
			}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return usagereporter.RunClient(ctx, cfg, *serve, os.Stdout, os.Stderr)
}
