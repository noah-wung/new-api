package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	flag.Parse()

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
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}
