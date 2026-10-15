package usagereporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// runCodexCollector translates an incremental JSONL scan into the shared event
// envelope. Cumulative baselines and scan checkpoints are Codex-specific.
func runCodexCollector(ctx context.Context, cfg Config, output, diagnostics io.Writer) error {
	root, err := ExpandPath(cfg.CodexDir)
	if err != nil {
		return err
	}
	configPath, err := ExpandPath(cfg.CodexConfigPath)
	if err != nil {
		return err
	}
	state, err := LoadState(cfg.StatePath)
	if err != nil {
		return err
	}
	state = state.PrepareForServer(cfg.Server)
	result, err := CollectCodexUsageWithWindow(root, state, cfg.AcceptWindowDays, CodexCollectionPolicy{
		GatewayURLs:       append([]string{cfg.Server}, cfg.GatewayURLs...),
		ExcludedProviders: cfg.ExcludedCodexProviders, ConfigPath: configPath,
	})
	if err != nil {
		return err
	}
	if result.ExcludedGatewayEvents > 0 {
		fmt.Fprintf(output, "excluded %d gateway-metered usage events\n", result.ExcludedGatewayEvents)
	}
	if result.DeferredProviderEvents > 0 {
		fmt.Fprintf(diagnostics, "warning: deferred %d usage events with unresolved providers; configure provider routes before reporting\n", result.DeferredProviderEvents)
	}
	excludedRoutes := make([]string, 0, len(cfg.ExcludedCodexProviders))
	for _, provider := range cfg.ExcludedCodexProviders {
		if provider = strings.TrimSpace(provider); provider != "" {
			excludedRoutes = append(excludedRoutes, sha256Hex(provider))
		}
	}
	reporter, err := NewReporter(ReporterConfig{
		Source: cfg.Source, Server: cfg.Server, Credential: cfg.Credential,
		StatePath: cfg.StatePath, AcceptWindowDays: cfg.AcceptWindowDays, GatewayURLs: cfg.GatewayURLs, ExcludedRoutes: excludedRoutes,
	})
	if err != nil {
		return err
	}
	events := make([]CollectedEvent, 0, len(result.Events))
	for _, event := range result.Events {
		events = append(events, CollectedEvent{Event: event, ClientVersion: result.ClientVersion, Origin: result.routes[event.EventID].Origin, RouteID: result.routes[event.EventID].ID})
	}
	if err := reporter.CheckStorage(); err != nil {
		return err
	}
	stats, err := reporter.Report(ctx, Collection{Events: events, Commit: func() error {
		result.NextState.Server = strings.TrimSpace(cfg.Server)
		return result.NextState.Save(cfg.StatePath)
	}})
	if err != nil {
		var delivery *DeliveryError
		if errors.As(err, &delivery) && ctx.Err() == nil {
			observeHealthy(ctx)
		}
		return err
	}
	observeHealthy(ctx)
	if stats.Sent == 0 {
		fmt.Fprintln(output, "no new usage events")
		return nil
	}
	fmt.Fprintf(output, "reported %d events (%d accepted, %d duplicate, %d rejected)\n", stats.Sent, stats.Accepted, stats.Duplicate, stats.Rejected)
	return nil
}
