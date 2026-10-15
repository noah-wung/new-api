package usagereporter

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const reporterQueueCapacity = 10000

var (
	ErrUsageConflict = errors.New("conflicting usage for the same request")
	ErrQueueFull     = errors.New("usage queue is full")
)

// ReporterConfig contains delivery policy only, independent of any client format.
// Nil ExternalURLs allows known non-gateway origins; [] denies every external origin.
type ReporterConfig struct {
	QueueCapacity    int
	Source           string
	Server           string
	Credential       string
	StatePath        string
	AcceptWindowDays int
	GatewayURLs      []string
	ExternalURLs     []string
	ExcludedRoutes   []string
}

type ReportStats struct {
	Sent      int
	Accepted  int
	Duplicate int
	Rejected  int
}

// Collection separates a client's scan checkpoint from delivery. The checkpoint
// advances only after durable enqueue and a successful flush; partial ACKs stay queued.
type Collection struct {
	Events []CollectedEvent
	Commit func() error
}

// Reporter owns route filtering, durable deduplication, batches, ACKs and retries.
// The queue namespace is isolated by source, server and reporting credential.
type Reporter struct {
	cfg          ReporterConfig
	policy       *RoutePolicy
	queueDir     string
	mu           sync.Mutex
	uploadMu     sync.Mutex
	collectMu    sync.Mutex
	confirmed    map[string]bool
	rememberACKs bool
}

func NewReporter(cfg ReporterConfig) (*Reporter, error) {
	return openReporter(cfg, filepath.Join(cfg.StatePath+".usage-queue", cfg.Source))
}

func openReporter(cfg ReporterConfig, queueRoot string) (*Reporter, error) {
	if err := (Config{Server: cfg.Server, Credential: cfg.Credential}).Validate(); err != nil {
		return nil, err
	}
	if cfg.Source == "" || cfg.StatePath == "" {
		return nil, errors.New("source and state path are required")
	}
	for _, ch := range cfg.Source {
		if ch != '_' && ch != '-' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			return nil, errors.New("invalid usage source")
		}
	}
	if cfg.QueueCapacity == 0 {
		cfg.QueueCapacity = reporterQueueCapacity
	}
	if cfg.QueueCapacity < 1 || cfg.QueueCapacity > reporterQueueCapacity {
		return nil, errors.New("queue capacity must be between 1 and 10000")
	}
	policy, err := NewRoutePolicy(cfg.Server, cfg.GatewayURLs, cfg.ExternalURLs, cfg.ExcludedRoutes...)
	if err != nil {
		return nil, err
	}
	namespace := sha256Hex(serverIdentity(cfg.Server) + "\x00" + cfg.Credential)
	dir := filepath.Join(queueRoot, namespace)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	confirmed := make(map[string]bool)
	data, err := os.ReadFile(filepath.Join(dir, ".confirmed"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(data) > 0 {
		if err := common.Unmarshal(data, &confirmed); err != nil || confirmed == nil {
			return nil, errors.New("could not decode confirmed usage")
		}
	}
	return &Reporter{cfg: cfg, policy: policy, queueDir: dir, confirmed: confirmed, rememberACKs: len(confirmed) > 0}, nil
}

func (c *Reporter) Report(ctx context.Context, collection Collection) (ReportStats, error) {
	c.collectMu.Lock()
	defer c.collectMu.Unlock()
	if collection.Commit != nil {
		c.mu.Lock()
		c.rememberACKs = true
		c.mu.Unlock()
	}
	stats := ReportStats{}
	for _, event := range collection.Events {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		err := c.Record(event)
		if errors.Is(err, ErrQueueFull) {
			partial, flushErr := c.FlushContext(ctx)
			stats.add(partial)
			if flushErr != nil {
				return stats, flushErr
			}
			err = c.Record(event)
		}
		if err != nil {
			return stats, err
		}
	}
	partial, err := c.FlushContext(ctx)
	stats.add(partial)
	if err != nil {
		return stats, err
	}
	if collection.Commit == nil {
		return stats, nil
	}
	if err := collection.Commit(); err != nil {
		return stats, err
	}
	// ACK receipts bridge a failed scan or checkpoint save. Once the adapter's
	// checkpoint is durable, only unacknowledged queue entries remain necessary.
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(filepath.Join(c.queueDir, ".confirmed")); err != nil && !os.IsNotExist(err) {
		return stats, err
	}
	c.confirmed = make(map[string]bool)
	c.rememberACKs = false
	return stats, nil
}

func (s *ReportStats) add(other ReportStats) {
	s.Sent += other.Sent
	s.Accepted += other.Accepted
	s.Duplicate += other.Duplicate
	s.Rejected += other.Rejected
}

func validateCollectedEvent(event UsageEvent, acceptWindowDays int) error {
	decodedID, err := hex.DecodeString(event.EventID)
	if err != nil || len(decodedID) != 32 {
		return errors.New("usage event ID must be a SHA-256 digest")
	}
	window := buildClientAcceptWindow(acceptWindowDays)
	if event.OccurredAt <= 0 || event.OccurredAt > time.Now().Unix()+300 || window.Enabled && event.OccurredAt < window.CutoffUnix {
		return errors.New("event time is outside the acceptance window")
	}
	if strings.TrimSpace(event.SourceModelName) == "" || len(event.SourceModelName) > 128 {
		return errors.New("missing or invalid model")
	}
	for _, count := range []int{event.InputTokens, event.CachedInputTokens, event.OutputTokens, event.ReasoningOutputTokens, event.TotalTokens} {
		if count < 0 || int64(count) > 2147483647 {
			return errors.New("invalid token count")
		}
	}
	if event.TotalTokens == 0 {
		return errors.New("invalid token total")
	}
	return nil
}

func (c *Reporter) Record(event CollectedEvent) error {
	origin, err := NormalizeUpstreamURL(event.Origin)
	if err != nil {
		return ErrUnapprovedRoute
	}
	event.Origin = origin
	if event.RouteID != "" {
		decoded, err := hex.DecodeString(event.RouteID)
		if err != nil || len(decoded) != 32 {
			return errors.New("route ID must be a SHA-256 digest")
		}
	}
	allowed, err := c.policy.Allows(origin, event.RouteID)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	if err := validateCollectedEvent(event.Event, c.cfg.AcceptWindowDays); err != nil {
		return err
	}
	if len(event.ClientVersion) > 64 {
		event.ClientVersion = ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.confirmed[event.Event.EventID] {
		return nil
	}
	path := filepath.Join(c.queueDir, event.Event.EventID+".json")
	previous, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		previous, err = os.ReadFile(path + ".rejected")
	}
	if err == nil {
		var queued CollectedEvent
		if err := common.Unmarshal(previous, &queued); err != nil {
			return errors.New("invalid queued event")
		}
		if queued.Event != event.Event || queued.Origin != event.Origin || queued.RouteID != event.RouteID {
			return ErrUsageConflict
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	files, err := os.ReadDir(c.queueDir)
	if err != nil {
		return err
	}
	queuedCount := 0
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".json") || strings.HasSuffix(file.Name(), ".json.rejected") {
			queuedCount++
		}
	}
	if queuedCount >= c.cfg.QueueCapacity {
		return ErrQueueFull
	}
	content, err := common.Marshal(event)
	if err != nil {
		return err
	}
	return writePrivateFile(path, content)
}

// Flush uploads the current queue in bounded batches. New events and incomplete
// ACKs wait for the next interval; only explicit acknowledgements allow removal.
func (c *Reporter) Flush() error {
	_, err := c.FlushContext(context.Background())
	return err
}

func (c *Reporter) FlushContext(ctx context.Context) (stats ReportStats, flushErr error) {
	c.uploadMu.Lock()
	defer c.uploadMu.Unlock()
	c.mu.Lock()
	files, err := os.ReadDir(c.queueDir)
	if err != nil {
		c.mu.Unlock()
		return stats, err
	}
	payload := ReportPayload{Source: c.cfg.Source}
	paths := make(map[string]string)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(c.queueDir, file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			c.mu.Unlock()
			return stats, err
		}
		var queued CollectedEvent
		if err := common.Unmarshal(data, &queued); err != nil {
			c.mu.Unlock()
			return stats, errors.New("could not decode queued usage")
		}
		allowed, policyErr := c.policy.Allows(queued.Origin, queued.RouteID)
		if policyErr != nil {
			continue
		}
		if !allowed {
			if err := os.Remove(path); err != nil {
				c.mu.Unlock()
				return stats, err
			}
			continue
		}
		payload.Events = append(payload.Events, queued.Event)
		payload.ClientVersion = queued.ClientVersion
		paths[queued.Event.EventID] = path
	}
	c.mu.Unlock()
	for offset := 0; offset < len(payload.Events); offset += 100 {
		batch := payload
		batch.Events = payload.Events[offset:min(offset+100, len(payload.Events))]
		batchPaths := make(map[string]string, len(batch.Events))
		for _, event := range batch.Events {
			batchPaths[event.EventID] = paths[event.EventID]
		}
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		ack, err := uploadReportWithClient(ctx, &http.Client{Timeout: 30 * time.Second}, c.cfg.Server, c.cfg.Credential, batch)
		if err != nil {
			return stats, &DeliveryError{Err: err}
		}
		stats.Sent += len(batch.Events)
		c.mu.Lock()
		if c.rememberACKs {
			for _, id := range append(ack.AcceptedEventIDs, ack.DuplicateEventIDs...) {
				if batchPaths[id] != "" {
					c.confirmed[id] = true
				}
			}
			data, err := common.Marshal(c.confirmed)
			if err == nil {
				err = writePrivateFile(filepath.Join(c.queueDir, ".confirmed"), data)
			}
			if err != nil {
				c.mu.Unlock()
				return stats, err
			}
		}
		for _, id := range ack.AcceptedEventIDs {
			if batchPaths[id] != "" {
				stats.Accepted++
			}
		}
		for _, id := range ack.DuplicateEventIDs {
			if batchPaths[id] != "" {
				stats.Duplicate++
			}
		}
		for _, id := range append(ack.AcceptedEventIDs, ack.DuplicateEventIDs...) {
			if path := batchPaths[id]; path != "" {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					c.mu.Unlock()
					return stats, err
				}
			}
		}
		for _, rejection := range ack.Rejected {
			if path := batchPaths[rejection.EventID]; path != "" {
				stats.Rejected++
				// Preserve rejected usage for diagnosis without a retry loop. Do
				// not persist arbitrary response text returned by a remote server.
				if err := os.Rename(path, path+".rejected"); err != nil && !os.IsNotExist(err) {
					c.mu.Unlock()
					return stats, err
				}
			}
		}
		c.mu.Unlock()
	}

	return stats, nil
}

// RunUploads is shared by streaming collectors; polling collectors use the
// same interval through the platform scheduler.
func (c *Reporter) RunUploads(ctx context.Context, diagnostics io.Writer) {
	if diagnostics == nil {
		diagnostics = io.Discard
	}
	ticker := time.NewTicker(DefaultReportInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := c.FlushContext(ctx); err != nil && ctx.Err() == nil {
			// Count-only generic diagnostics; network errors can include URLs.
			fmt.Fprintln(diagnostics, "warning: usage upload failed; queued events retained")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
