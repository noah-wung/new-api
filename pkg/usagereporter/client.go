package usagereporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// Client registrations contain lifecycle differences only. New adapters use
// Reporter.Record (streaming) or Reporter.Report (polling), never HTTP uploads.
type clientRegistration struct {
	selfCheck         func(Config) error
	legacyQueueSuffix string
	streaming         bool
	run               func(context.Context, Config, io.Writer, io.Writer) error
}

var clientRegistrations = map[string]clientRegistration{
	SourceCodex: {run: runCodexCollector, selfCheck: checkCodexLocalData},
	SourceClaudeCode: {streaming: true, selfCheck: checkClaudeLocalConfig, legacyQueueSuffix: ".claude-queue", run: func(ctx context.Context, cfg Config, _ io.Writer, diagnostics io.Writer) error {
		return RunClaudeReceiver(ctx, cfg, diagnostics)
	}},
}

// RunClient selects one adapter; the CLI does not own client delivery logic.
func RunClient(ctx context.Context, cfg Config, serve bool, output, diagnostics io.Writer) error {
	cfg = cfg.WithDefaults()
	if output == nil {
		output = io.Discard
	}
	if diagnostics == nil {
		diagnostics = io.Discard
	}
	registration, ok := clientRegistrations[cfg.Source]
	if !ok {
		return fmt.Errorf("unsupported usage source %q", cfg.Source)
	}
	if registration.streaming != serve {
		return errors.New("streaming collectors require --serve; polling collectors run without --serve")
	}
	var err error
	cfg.StatePath, err = ExpandPath(cfg.StatePath)
	if err != nil {
		return err
	}
	if registration.streaming {
		return registration.run(ctx, cfg, output, diagnostics)
	}
	release, err := AcquireRunLock(cfg.StatePath)
	if errors.Is(err, ErrReporterAlreadyRunning) {
		fmt.Fprintln(output, "reporter already running")
		return nil
	}
	if err != nil {
		return err
	}
	defer func() {
		if err := release(); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(diagnostics, "warning: could not release reporter lock")
		}
	}()
	return registration.run(ctx, cfg, output, diagnostics)
}
