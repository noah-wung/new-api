package usagereporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const SourceZCode = "zcode"

const zcodeReportChunkSize = 100

func zcodeRoutePolicy(cfg Config) (*RoutePolicy, error) {
	policy, err := NewRoutePolicy(cfg.Server, cfg.GatewayURLs, nil)
	if err != nil {
		return nil, err
	}
	if err := policy.AddGatewayHosts(cfg.ZCodeGatewayHosts); err != nil {
		return nil, err
	}
	server, err := url.Parse(cfg.Server)
	if err != nil || server.Hostname() == "" {
		return nil, errors.New("invalid reporter server URL")
	}
	host := server.Hostname()
	if net.ParseIP(host) != nil {
		policy.gatewayHosts[strings.ToLower(host)] = true
	} else if err := policy.AddGatewayHosts([]string{host}); err != nil {
		return nil, err
	}
	return policy, nil
}

func checkZCodeLocalData(cfg Config) error {
	_, err := zcodeRoutePolicy(cfg)
	if err != nil {
		return err
	}
	if _, err := zcodeCurrentProviderOrigins(cfg); err != nil {
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
		namespace := sha256Hex(serverIdentity(cfg.Server) + "\x00" + cfg.Credential)
		state, err := loadZCodeState(path, namespace)
		if err != nil {
			return err
		}
		archive, err := openZCodeArchive(path, namespace, false, true)
		if err != nil {
			return err
		}
		if err := state.validateArchive(archive); err != nil {
			_ = archive.close()
			return err
		}
		return archive.close()
	}
	return nil
}

func runZCodeCollector(ctx context.Context, cfg Config, output, diagnostics io.Writer) error {
	namespace := sha256Hex(serverIdentity(cfg.Server) + "\x00" + cfg.Credential)
	state, err := loadZCodeState(cfg.StatePath, namespace)
	if err != nil {
		return err
	}
	state.archive, err = openZCodeArchive(cfg.StatePath, namespace, false, false)
	if err != nil {
		return err
	}
	if err := state.validateArchive(state.archive); err != nil {
		_ = state.archive.close()
		return err
	}
	defer func() { _ = state.archive.close() }()
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
	policy, err := zcodeRoutePolicy(cfg)
	if err != nil {
		return err
	}
	currentRoutes, err := zcodeCurrentProviderOrigins(cfg)
	if err != nil {
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
		origin, allowed, resolved, err := resolveZCodeCurrentRoute(policy, currentRoutes, cfg.ZCodeProviderRoutes, cfg.ZCodeExcludedProviders, snapshot.ProviderID, snapshot.StartedAt, snapshot.Event.OccurredAt)
		return err == nil && resolved && allowed && origin == event.Origin, err
	}
	onResults := func(results []ReporterResult) error {
		for _, result := range results {
			snapshot, ok := state.Snapshots[result.EventID]
			if !ok {
				return errors.New("ZCode acknowledgement has no snapshot")
			}
			snapshot.Submitted, snapshot.Delivery = true, result.Result
			state.Snapshots[result.EventID] = snapshot
		}
		return state.save(cfg.StatePath)
	}
	healthy := false
	markHealthy := func() {
		if !healthy && ctx.Err() == nil {
			healthy = true
			observeHealthy(ctx)
		}
	}
	reporter, err := NewReporter(ReporterConfig{Source: SourceZCode, Server: cfg.Server, Credential: cfg.Credential, StatePath: cfg.StatePath, AcceptWindowDays: cfg.AcceptWindowDays, GatewayURLs: cfg.GatewayURLs, GatewayHosts: cfg.ZCodeGatewayHosts, RecheckRoute: recheck, OnResults: onResults, OnProgress: markHealthy, StopOnQueueFull: true})
	if err != nil {
		return err
	}
	if err := reporter.CheckStorage(); err != nil {
		return err
	}
	queuedStats, err := reporter.FlushContext(ctx)
	if err != nil {
		var delivery *DeliveryError
		if errors.As(err, &delivery) {
			markHealthy()
		}
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
		origin, allowed, resolved, err := resolveZCodeCurrentRoute(policy, currentRoutes, cfg.ZCodeProviderRoutes, cfg.ZCodeExcludedProviders, snapshot.ProviderID, snapshot.StartedAt, snapshot.Event.OccurredAt)
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
	stats := queuedStats
	for offset := 0; offset < len(events) || offset == 0; offset += zcodeReportChunkSize {
		end := min(offset+zcodeReportChunkSize, len(events))
		ids := submitted[offset:end]
		partial, reportErr := reporter.Report(ctx, Collection{Events: events[offset:end], Commit: func() error {
			for _, id := range ids {
				snapshot := state.Snapshots[id]
				snapshot.Submitted = true
				if snapshot.Delivery == "" {
					snapshot.Delivery = "queued"
				}
				state.Snapshots[id] = snapshot
			}
			return state.save(cfg.StatePath)
		}})
		stats.add(partial)
		if reportErr != nil {
			err = reportErr
			break
		}
		markHealthy()
		if end == len(events) {
			break
		}
	}
	fmt.Fprintf(output, "ZCode: %d sent, %d accepted, %d duplicate, %d rejected; %d unresolved, %d excluded, %d pending, %d invalid, %d conflicts, %d outside window\n", stats.Sent, stats.Accepted, stats.Duplicate, stats.Rejected, unresolved, excluded, pending, invalid, conflicts, outsideWindow)
	if conflicts > 0 || invalid > 0 {
		fmt.Fprintln(diagnostics, "ZCode usage requires local review; totals may be incomplete")
	}
	var delivery *DeliveryError
	if err == nil || errors.As(err, &delivery) && ctx.Err() == nil {
		markHealthy()
		if archiveErr := state.compact(cfg.StatePath); archiveErr != nil {
			return archiveErr
		}
	}
	return err
}
