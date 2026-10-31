package usagereporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const SourceZCode = "zcode"

func checkZCodeLocalData(cfg Config) error {
	policy, err := NewRoutePolicy(cfg.Server, cfg.GatewayURLs, nil)
	if err != nil {
		return err
	}
	if err := policy.AddGatewayHosts(cfg.ZCodeGatewayHosts); err != nil {
		return err
	}
	_, err = readZCodeUsagePage(context.Background(), cfg.ZCodeDBPath, 0, 1)
	if err != nil {
		return err
	}
	if cfg.StatePath != "" && cfg.Server != "" && cfg.Credential != "" {
		path, err := ExpandPath(cfg.StatePath)
		if err != nil {
			return err
		}
		_, err = loadZCodeState(path, sha256Hex(serverIdentity(cfg.Server)+"\x00"+cfg.Credential))
		return err
	}
	return nil
}

func runZCodeCollector(ctx context.Context, cfg Config, output, diagnostics io.Writer) error {
	namespace := sha256Hex(serverIdentity(cfg.Server) + "\x00" + cfg.Credential)
	state, err := loadZCodeState(cfg.StatePath, namespace)
	if err != nil {
		return err
	}
	queueRoot := filepath.Join(cfg.StatePath+".usage-queue", SourceZCode)
	entries, err := os.ReadDir(queueRoot)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != namespace {
			return errors.New("a previous ZCode queue namespace exists; migrate it explicitly before changing credentials")
		}
	}
	policy, err := NewRoutePolicy(cfg.Server, cfg.GatewayURLs, nil)
	if err != nil {
		return err
	}
	if err := policy.AddGatewayHosts(cfg.ZCodeGatewayHosts); err != nil {
		return err
	}
	window := buildClientAcceptWindow(cfg.AcceptWindowDays)
	invalid, pending, outsideWindow := 0, 0, 0
	for offset := 0; ; offset += 1000 {
		rows, err := readZCodeUsagePage(ctx, cfg.ZCodeDBPath, offset, 1000)
		if err != nil {
			return err
		}
		for _, row := range rows {
			event, wait, err := normalizeZCodeUsage(row)
			if wait {
				pending++
				continue
			}
			if err != nil {
				invalid++
				if row.ID == "" || row.SessionID == "" {
					continue
				}
				key := sha256Hex("zcode:model_usage:v1\x00" + row.SessionID + "\x00" + row.ID)
				if _, exists := state.Invalid[key]; !exists && len(state.Invalid)+len(state.Snapshots) >= zcodeSnapshotLimit {
					return errors.New("ZCode snapshot capacity reached")
				}
				state.Invalid[key] = zcodeInvalid{Input: row.Input, Output: row.Output, Reasoning: row.Reasoning, CacheWrite: row.CacheWrite, CacheRead: row.CacheRead, Total: row.Total, InvalidNumeric: row.InvalidNumeric}
				continue
			}
			if event.EventID == "" {
				continue
			}
			if event.OccurredAt <= 0 || window.Enabled && (event.OccurredAt < window.CutoffUnix || event.OccurredAt > window.FutureLimit) {
				outsideWindow++
				continue
			}
			if err := state.observe(event, row.ProviderID, row.StartedAt.Int64/1000); err != nil {
				return err
			}
		}
		if err := state.save(cfg.StatePath); err != nil {
			return err
		}
		if len(rows) < 1000 {
			break
		}
	}
	state.InvalidCount = invalid
	if err := state.save(cfg.StatePath); err != nil {
		return err
	}

	recheck := func(event CollectedEvent) (bool, error) {
		snapshot, ok := state.Snapshots[event.Event.EventID]
		if !ok || snapshot.Conflict != nil || snapshot.Event != event.Event {
			return false, nil
		}
		origin, allowed, resolved, err := resolveZCodeRoute(policy, cfg.ZCodeProviderRoutes, cfg.ZCodeExcludedProviders, snapshot.ProviderID, snapshot.StartedAt, snapshot.Event.OccurredAt)
		return err == nil && resolved && allowed && origin == event.Origin, err
	}
	onResult := func(id, result string) error {
		snapshot, ok := state.Snapshots[id]
		if !ok {
			return errors.New("ZCode acknowledgement has no snapshot")
		}
		snapshot.Submitted, snapshot.Delivery = true, result
		state.Snapshots[id] = snapshot
		return state.save(cfg.StatePath)
	}
	reporter, err := NewReporter(ReporterConfig{Source: SourceZCode, Server: cfg.Server, Credential: cfg.Credential, StatePath: cfg.StatePath, AcceptWindowDays: cfg.AcceptWindowDays, GatewayURLs: cfg.GatewayURLs, GatewayHosts: cfg.ZCodeGatewayHosts, RecheckRoute: recheck, OnResult: onResult, StopOnQueueFull: true})
	if err != nil {
		return err
	}
	if err := reporter.CheckStorage(); err != nil {
		return err
	}
	queuedStats, err := reporter.FlushContext(ctx)
	if err != nil {
		return err
	}
	var events []CollectedEvent
	var submitted []string
	unresolved, excluded, conflicts := 0, 0, 0
	for id, snapshot := range state.Snapshots {
		if snapshot.Conflict != nil {
			conflicts++
			continue
		}
		if snapshot.Submitted {
			continue
		}
		origin, allowed, resolved, err := resolveZCodeRoute(policy, cfg.ZCodeProviderRoutes, cfg.ZCodeExcludedProviders, snapshot.ProviderID, snapshot.StartedAt, snapshot.Event.OccurredAt)
		if err != nil {
			return err
		}
		if !resolved {
			unresolved++
			continue
		}
		if !allowed {
			excluded++
			continue
		}
		if err := validateCollectedEvent(snapshot.Event, cfg.AcceptWindowDays); err != nil {
			unresolved++
			continue
		}
		snapshot.Held = true
		state.Snapshots[id] = snapshot
		submitted = append(submitted, id)
		events = append(events, CollectedEvent{Event: snapshot.Event, RouteID: sha256Hex(snapshot.ProviderID), Origin: origin})
	}
	if err := state.save(cfg.StatePath); err != nil {
		return err
	}
	stats, err := reporter.Report(ctx, Collection{Events: events, Commit: func() error {
		for _, id := range submitted {
			snapshot := state.Snapshots[id]
			snapshot.Submitted = true
			if snapshot.Delivery == "" {
				snapshot.Delivery = "queued"
			}
			state.Snapshots[id] = snapshot
		}
		return state.save(cfg.StatePath)
	}})
	stats.add(queuedStats)
	fmt.Fprintf(output, "ZCode: %d sent, %d accepted, %d duplicate, %d rejected; %d unresolved, %d excluded, %d pending, %d invalid, %d conflicts, %d outside window\n", stats.Sent, stats.Accepted, stats.Duplicate, stats.Rejected, unresolved, excluded, pending, invalid, conflicts, outsideWindow)
	if conflicts > 0 || invalid > 0 {
		fmt.Fprintln(diagnostics, "ZCode usage requires local review; totals may be incomplete")
	}
	var delivery *DeliveryError
	if err == nil || errors.As(err, &delivery) && ctx.Err() == nil {
		observeHealthy(ctx)
	}
	return err
}
