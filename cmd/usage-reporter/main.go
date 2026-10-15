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

func runReporter() error {
	configPath := flag.String("config", "", "Reporter config file")
	preservePolicyFrom := flag.String("preserve-gateway-policy-from", "", "Copy gateway policy from an existing config into --config and exit")
	server := flag.String("server", "", "External usage API server URL")
	credential := flag.String("credential", "", "Dedicated reporting credential")
	source := flag.String("source", usagereporter.SourceCodex, "Usage source to report")
	codexDir := flag.String("codex-dir", "~/.codex/sessions", "Codex sessions directory")
	statePath := flag.String("state", "~/.new-api-usage-reporter/state.json", "Reporter state file")
	serveClaude := flag.Bool("serve", false, "Run the Claude Code OTLP HTTP/JSON receiver until interrupted")
	configureClaude := flag.Bool("configure-claude", false, "Merge telemetry into Claude user settings and exit")
	claudeSettings := flag.String("claude-settings", "", "Claude user settings.json (defaults to CLAUDE_CONFIG_DIR/settings.json or ~/.claude/settings.json)")
	claudeUpstream := flag.String("claude-upstream", "", "Explicit effective upstream URL for the Claude telemetry profile")
	installClaude := flag.Bool("install-claude", false, "Exchange a one-time Claude install token and configure telemetry")
	installToken := flag.String("install-token", "", "One-time Claude installer token")
	flag.Parse()
	if *installClaude {
		if *configPath == "" || *server == "" || *installToken == "" {
			return errors.New("--install-claude requires --server, --install-token and --config")
		}
		path, err := expandPath(*configPath)
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
		settings, err = expandPath(settings)
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
		expandedConfigPath, err = expandPath(trimmedConfigPath)
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
		previousPath, err := expandPath(*preservePolicyFrom)
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
		if *serveClaude {
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
		settingsPath, err := expandPath(settingsPath)
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

	if cfg.Source == usagereporter.SourceClaudeCode {
		if !*serveClaude {
			return errors.New("Claude Code collection requires --serve (OTLP HTTP/JSON); transcript scanning is not enabled")
		}
		expandedStatePath, err := expandPath(cfg.StatePath)
		if err != nil {
			return err
		}
		cfg.StatePath = expandedStatePath
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return usagereporter.RunClaudeReceiver(ctx, cfg, os.Stderr)
	}
	if *serveClaude {
		return errors.New("--serve requires source claude_code")
	}

	expandedCodexDir, err := expandPath(cfg.CodexDir)
	if err != nil {
		return err
	}
	expandedStatePath, err := expandPath(cfg.StatePath)
	if err != nil {
		return err
	}
	releaseLock, err := usagereporter.AcquireRunLock(expandedStatePath)
	if err != nil {
		if errors.Is(err, usagereporter.ErrReporterAlreadyRunning) {
			fmt.Println("reporter already running")
			return nil
		}
		return err
	}
	defer func() {
		if releaseErr := releaseLock(); releaseErr != nil && !os.IsNotExist(releaseErr) {
			_, _ = fmt.Fprintf(os.Stderr, "warning: could not release reporter lock: %v\n", releaseErr)
		}
	}()

	state, err := usagereporter.LoadState(expandedStatePath)
	if err != nil {
		return err
	}
	state = state.PrepareForServer(cfg.Server)
	expandedCodexConfigPath, err := expandPath(cfg.CodexConfigPath)
	if err != nil {
		return err
	}
	policy := usagereporter.CodexCollectionPolicy{
		GatewayURLs:       append([]string{cfg.Server}, cfg.GatewayURLs...),
		ExcludedProviders: cfg.ExcludedCodexProviders,
		ConfigPath:        expandedCodexConfigPath,
	}
	result, err := usagereporter.CollectUsage(cfg.Source, expandedCodexDir, state, cfg.AcceptWindowDays, policy)
	if err != nil {
		return err
	}
	if result.ExcludedGatewayEvents > 0 {
		fmt.Printf("excluded %d gateway-metered usage events\n", result.ExcludedGatewayEvents)
	}
	if result.DeferredProviderEvents > 0 {
		fmt.Fprintf(os.Stderr, "warning: deferred %d usage events with unresolved providers; configure provider routes before reporting\n", result.DeferredProviderEvents)
	}
	if len(result.Events) == 0 {
		// No external events need a server ACK. Persist locally filtered progress
		// as well, so the next run keeps the correct cumulative baseline.
		result.NextState.Server = strings.TrimSpace(cfg.Server)
		if err := result.NextState.Save(expandedStatePath); err != nil {
			return err
		}
		fmt.Println("no new usage events")
		return nil
	}

	ack, err := usagereporter.UploadReport(cfg.Server, cfg.Credential, usagereporter.ReportPayload{
		Source:        cfg.Source,
		ClientVersion: result.ClientVersion,
		Events:        result.Events,
	})
	if err != nil {
		return err
	}
	usagereporter.ApplyAcknowledgements(state, result.NextState, ack)
	state.Server = strings.TrimSpace(cfg.Server)
	if err := state.Save(expandedStatePath); err != nil {
		return err
	}

	fmt.Printf("reported %d events (%d accepted, %d duplicate, %d rejected)\n",
		len(result.Events),
		len(ack.AcceptedEventIDs),
		len(ack.DuplicateEventIDs),
		len(ack.Rejected),
	)
	return nil
}

func expandPath(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return "", errors.New("only the current user's home directory can be expanded")
	}
	return filepath.Join(home, path[2:]), nil
}
