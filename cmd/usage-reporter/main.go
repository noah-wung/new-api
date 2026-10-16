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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
)

func main() {
	if err := runReporter(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runReporter() (runErr error) {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	showVersion := flag.Bool("version", false, "Write reporter version information")
	checkVersion := flag.Bool("check-version", false, "Check for a collector update and prompt for manual installation")
	selfCheck := flag.Bool("self-check", false, "Read-only local configuration and queue compatibility check")
	managed := flag.Bool("managed", false, "Enable private lifecycle control for a managed worker")
	transactionID := flag.String("transaction", "", "Managed worker transaction identity")
	updateCheck := flag.Bool("update-check", false, "Run due signed update check in the background")
	updateNow := flag.Bool("update-now", false, "Check and apply a signed update immediately")
	updateApply := flag.String("update-apply", "", "Apply an already prepared transaction")
	updateRecover := flag.Bool("update-recover", false, "Recover an interrupted update")
	autoUpdate := flag.String("auto-update", "", "Set automatic updates: on or off")
	runtimePath := flag.String("runtime", "", "Managed runtime directory")
	installAll := flag.Bool("install-clients", false, "Install selected collectors with signed update support")
	codexInstallToken := flag.String("codex-install-token", "", "One-time Codex installation token")
	claudeInstallToken := flag.String("claude-install-token", "", "One-time Claude installation token")
	trustKeys := flag.String("trust-keys", "", "Trusted public keys JSON file")
	catalogFile := flag.String("catalog", "", "Signed installation catalog file")
	installAutomatic := flag.Bool("install-auto-update", true, "Enable automatic checks after installation")
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
	_ = flag.String("claude-upstream", "", "Deprecated; routing is detected automatically")
	claudeHeaders := flag.Bool("claude-otel-headers", false, "Emit runtime Claude telemetry routing headers")
	installClaude := flag.Bool("install-claude", false, "Exchange a one-time Claude install token and configure telemetry")
	installCodex := flag.Bool("install-codex", false, "Exchange an installation token, preserving existing Codex configuration")
	installToken := flag.String("install-token", "", "One-time client installer token")
	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *claudeHeaders {
		if *runtimePath != "" {
			binary, err := reporterupdate.CurrentBinary(*runtimePath)
			if err != nil {
				return err
			}
			command := exec.Command(binary, "--claude-otel-headers")
			command.Stdout, command.Stderr = os.Stdout, os.Stderr
			return command.Run()
		}
		content, err := common.Marshal(usagereporter.ClaudeTelemetryHeaders())
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(content, '\n'))
		return err
	}
	if *logPath != "" {
		path, err := usagereporter.ExpandPath(*logPath)
		if err != nil {
			return err
		}
		logFile, err := reporterupdate.OpenLog(path)
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
	if *showVersion {
		fmt.Printf("%s %s %s %s\n", reporterupdate.Version, reporterupdate.Commit, reporterupdate.BuildReleaseID, reporterupdate.SchemaDescription())
		return nil
	}
	if *installAll {
		return installClients(*server, *codexInstallToken, *claudeInstallToken, *trustKeys, *catalogFile, *installAutomatic)
	}
	if *updateCheck || *updateNow || *updateApply != "" || *updateRecover || *autoUpdate != "" {
		return runUpdate(*runtimePath, *updateApply, *updateCheck || *updateNow, *updateRecover, *updateNow, *autoUpdate)
	}
	if *installClaude || *installCodex {
		if *configPath == "" || *server == "" || *installToken == "" {
			return errors.New("installation requires --server, --install-token and --config")
		}
		path, err := usagereporter.ExpandPath(*configPath)
		if err != nil {
			return err
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		owned, err := reporterupdate.ManagedProfile(filepath.Join(home, ".new-api-usage-reporter", "runtime"), path)
		if err != nil {
			return err
		}
		if owned {
			return errors.New("this collector is managed; use a signed installation command or --auto-update off instead of a legacy installer")
		}
		if *installCodex {
			return usagereporter.InstallCodexReporter(*server, *installToken, path)
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

	if *selfCheck {
		return usagereporter.SelfCheck(cfg)
	}
	if *checkVersion {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return checkCollectorVersion(ctx, cfg)
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
		cfg.ClaudeAutoRoute = true
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
		binaryPath, err := os.Executable()
		if err != nil {
			return err
		}
		if err := usagereporter.ConfigureClaudeTelemetry(cfg, settingsPath, binaryPath); err != nil {
			return err
		}
		if err := usagereporter.SaveConfig(expandedConfigPath, cfg); err != nil {
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
	if *managed {
		if expandedConfigPath == "" {
			return errors.New("managed workers require a configuration file")
		}
		health := reporterupdate.Health{PID: os.Getpid(), ReleaseID: reporterupdate.BuildReleaseID, Source: cfg.Source, Transaction: *transactionID}
		ready, closeControl, err := reporterupdate.ServeControl(ctx, expandedConfigPath, health, stop)
		if err != nil {
			return err
		}
		defer closeControl()
		ctx = usagereporter.WithHealthObserver(ctx, func() {
			ready()
			health.Ready = true
			_ = reporterupdate.Save(expandedConfigPath+".health.json", health)
		})
	}
	if *managed {
		return usagereporter.RunClient(ctx, cfg, *serve, os.Stdout, os.Stderr)
	}
	return runClientWithNotices(ctx, cfg, *serve)
}
