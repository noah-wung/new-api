package usagereporter

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const claudeQueueCapacity = 10000

var errClaudeUsageConflict = errors.New("conflicting usage for the same request")

// ClaudeReceiver implements the OTLP HTTP/JSON logs endpoint. A successful
// response means the normalized event is already on disk, not merely in RAM.
type ClaudeReceiver struct {
	cfg      Config
	policy   *claudeRoutePolicy
	queueDir string
	mu       sync.Mutex
	uploadMu sync.Mutex
}

func NewClaudeReceiver(cfg Config) (*ClaudeReceiver, error) {
	if cfg.Source != SourceClaudeCode {
		return nil, errors.New("Claude receiver requires source claude_code")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(cfg.ClaudeReceiverToken) < 32 || strings.ContainsAny(cfg.ClaudeReceiverToken, " \t\r\n,") {
		return nil, errors.New("claude_receiver_token must contain at least 32 characters without whitespace or commas")
	}
	if _, err := claudeListenAddress(cfg); err != nil {
		return nil, err
	}
	policy, err := newClaudeRoutePolicy(cfg)
	if err != nil {
		return nil, err
	}
	// Credentials bind the queue to a reporting identity, preventing a changed
	// configuration from uploading another account's pending usage.
	namespace := sha256Hex(serverIdentity(cfg.Server) + "\x00" + cfg.Credential)
	dir := filepath.Join(cfg.StatePath+".claude-queue", namespace)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &ClaudeReceiver{cfg: cfg, policy: policy, queueDir: dir}, nil
}

func (c *ClaudeReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/logs" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.cfg.ClaudeReceiverToken)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" || r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
		http.Error(w, "OTLP HTTP/JSON required, without compression", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		http.Error(w, "OTLP body exceeds limit", http.StatusRequestEntityTooLarge)
		return
	}
	var request otlpLogsRequest
	if err := common.Unmarshal(body, &request); err != nil {
		http.Error(w, "invalid OTLP JSON", http.StatusBadRequest)
		return
	}
	rejected := 0
	window := buildClientAcceptWindow(c.cfg.AcceptWindowDays)
	for _, resource := range request.ResourceLogs {
		for _, scope := range resource.ScopeLogs {
			for _, record := range scope.LogRecords {
				event, err := normalizeClaudeRequest(record, resource.Resource.Attributes, c.policy, window)
				if err != nil {
					rejected++
					continue
				}
				if event == nil {
					continue
				}
				if err := c.enqueue(*event); err != nil {
					if errors.Is(err, errClaudeUsageConflict) {
						rejected++
						continue
					}
					// The exporter can retry this entire batch. Persisted records
					// use stable identities so partial storage is safe to replay.
					http.Error(w, "could not persist usage; retry export", http.StatusServiceUnavailable)
					return
				}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if rejected > 0 {
		// OTLP partial success is intentionally not retried by exporters. The
		// count-only reason avoids echoing attributes, paths or credentials.
		fmt.Fprintf(w, `{"partialSuccess":{"rejectedLogRecords":"%d","errorMessage":"usage records require an approved route, valid time and token counts"}}`, rejected)
		return
	}
	_, _ = io.WriteString(w, "{}")
}

func (c *ClaudeReceiver) enqueue(event claudeQueuedEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := filepath.Join(c.queueDir, event.Event.EventID+".json")
	if previous, err := os.ReadFile(path); err == nil {
		var queued claudeQueuedEvent
		if err := common.Unmarshal(previous, &queued); err != nil {
			return errors.New("invalid queued event")
		}
		if queued.Event != event.Event || queued.Origin != event.Origin {
			return errClaudeUsageConflict
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	files, err := os.ReadDir(c.queueDir)
	if err != nil {
		return err
	}
	if len(files) >= claudeQueueCapacity {
		return errors.New("usage queue is full")
	}
	content, err := common.Marshal(event)
	if err != nil {
		return err
	}
	return writePrivateFile(path, content)
}

// Flush uploads the current queue in bounded batches. New events and incomplete
// ACKs wait for the next interval; only explicit acknowledgements allow removal.
func (c *ClaudeReceiver) Flush() error {
	c.uploadMu.Lock()
	defer c.uploadMu.Unlock()
	c.mu.Lock()
	files, err := os.ReadDir(c.queueDir)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	payload := ReportPayload{Source: SourceClaudeCode}
	paths := make(map[string]string)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(c.queueDir, file.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			c.mu.Unlock()
			return err
		}
		var queued claudeQueuedEvent
		if err := common.Unmarshal(data, &queued); err != nil {
			c.mu.Unlock()
			return errors.New("could not decode queued usage")
		}
		if c.policy.gateways[queued.Origin] {
			if err := os.Remove(path); err != nil {
				c.mu.Unlock()
				return err
			}
			continue
		}
		if !c.policy.external[queued.Origin] {
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
		ack, err := UploadReport(c.cfg.Server, c.cfg.Credential, batch)
		if err != nil {
			return err
		}
		c.mu.Lock()
		for _, id := range append(ack.AcceptedEventIDs, ack.DuplicateEventIDs...) {
			if path := batchPaths[id]; path != "" {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					c.mu.Unlock()
					return err
				}
			}
		}
		for _, rejection := range ack.Rejected {
			if path := batchPaths[rejection.EventID]; path != "" {
				// Preserve rejected usage for diagnosis without a retry loop. Do
				// not persist arbitrary response text returned by a remote server.
				if err := os.Rename(path, path+".rejected"); err != nil && !os.IsNotExist(err) {
					c.mu.Unlock()
					return err
				}
			}
		}
		c.mu.Unlock()
	}

	return nil
}

// runUploads reports at startup and on the same five-minute cadence as Codex.
func (c *ClaudeReceiver) runUploads(ctx context.Context, diagnostics io.Writer) {
	ticker := time.NewTicker(DefaultReportInterval)
	defer ticker.Stop()
	for {
		if err := c.Flush(); err != nil {
			// Count-only generic diagnostics; network errors can include URLs.
			fmt.Fprintln(diagnostics, "warning: Claude usage upload failed; queued events retained")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func RunClaudeReceiver(ctx context.Context, cfg Config, diagnostics io.Writer) error {
	address, err := claudeListenAddress(cfg)
	if err != nil {
		return err
	}
	collector, err := NewClaudeReceiver(cfg)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: collector, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		collector.runUploads(workerCtx, diagnostics)
	}()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-workerCtx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(diagnostics, "Claude usage receiver listening on %s\n", address)
	err = server.Serve(listener)
	cancel()
	<-workerDone
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// writePrivateFile atomically replaces a private file after syncing its data.
// An interrupted write leaves the previous config/event intact.
func writePrivateFile(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".usage-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
