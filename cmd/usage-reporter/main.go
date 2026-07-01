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
	configPath := flag.String("config", "", "Reporter config file")
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
			exitWithError(err)
		}
		loadedConfig, err := usagereporter.LoadConfig(expandedConfigPath)
		if err != nil {
			exitWithError(err)
		}
		cfg = usagereporter.ApplyExplicitOverrides(*loadedConfig, overrideConfig, explicitFlags)
	} else if err := cfg.Validate(); err != nil {
		exitWithError(err)
	}

	if expandedConfigPath != "" {
		client := &http.Client{Timeout: 15 * time.Second}
		changed, err := usagereporter.BackfillAcceptWindowDaysFromServer(client, &cfg)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: could not backfill accept_window_days: %v\n", err)
		} else if changed {
			if err := usagereporter.SaveConfig(expandedConfigPath, cfg); err != nil {
				exitWithError(err)
			}
		}
	}

	expandedCodexDir, err := expandPath(cfg.CodexDir)
	if err != nil {
		exitWithError(err)
	}
	expandedStatePath, err := expandPath(cfg.StatePath)
	if err != nil {
		exitWithError(err)
	}
	releaseLock, err := usagereporter.AcquireRunLock(expandedStatePath)
	if err != nil {
		if errors.Is(err, usagereporter.ErrReporterAlreadyRunning) {
			fmt.Println("reporter already running")
			return
		}
		exitWithError(err)
	}
	defer func() {
		if releaseErr := releaseLock(); releaseErr != nil && !os.IsNotExist(releaseErr) {
			_, _ = fmt.Fprintf(os.Stderr, "warning: could not release reporter lock: %v\n", releaseErr)
		}
	}()

	state, err := usagereporter.LoadState(expandedStatePath)
	if err != nil {
		exitWithError(err)
	}
	state = state.PrepareForServer(cfg.Server)
	result, err := usagereporter.CollectUsage(cfg.Source, expandedCodexDir, state, cfg.AcceptWindowDays)
	if err != nil {
		exitWithError(err)
	}
	if len(result.Events) == 0 {
		fmt.Println("no new usage events")
		return
	}

	ack, err := usagereporter.UploadReport(cfg.Server, cfg.Credential, usagereporter.ReportPayload{
		Source:        cfg.Source,
		ClientVersion: result.ClientVersion,
		Events:        result.Events,
	})
	if err != nil {
		exitWithError(err)
	}
	usagereporter.ApplyAcknowledgements(state, result.NextState, ack)
	state.Server = strings.TrimSpace(cfg.Server)
	if err := state.Save(expandedStatePath); err != nil {
		exitWithError(err)
	}

	fmt.Printf("reported %d events (%d accepted, %d duplicate, %d rejected)\n",
		len(result.Events),
		len(ack.AcceptedEventIDs),
		len(ack.DuplicateEventIDs),
		len(ack.Rejected),
	)
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

func exitWithError(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
