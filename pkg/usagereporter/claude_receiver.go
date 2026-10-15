package usagereporter

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ClaudeReceiver only owns the OTLP transport and local authentication. Delivery
// and route policy are provided by the same Reporter used by polling collectors.
type ClaudeReceiver struct {
	*Reporter
	cfg Config
}

func claudeReportingConfig(cfg Config) ReporterConfig {
	external := cfg.ClaudeExternalURLs
	if external == nil {
		external = []string{"https://api.anthropic.com"}
	}
	return ReporterConfig{Source: cfg.Source, Server: cfg.Server, Credential: cfg.Credential,
		StatePath: cfg.StatePath, AcceptWindowDays: cfg.AcceptWindowDays,
		GatewayURLs: cfg.GatewayURLs, ExternalURLs: external}
}

func NewClaudeReceiver(cfg Config) (*ClaudeReceiver, error) {
	if cfg.Source != SourceClaudeCode {
		return nil, errors.New("Claude receiver requires source claude_code")
	}
	if len(cfg.ClaudeReceiverToken) < 32 || strings.ContainsAny(cfg.ClaudeReceiverToken, " \t\r\n,") {
		return nil, errors.New("claude_receiver_token must contain at least 32 characters without whitespace or commas")
	}
	if _, err := claudeListenAddress(cfg); err != nil {
		return nil, err
	}
	reporter, err := openReporter(claudeReportingConfig(cfg), cfg.StatePath+".claude-queue")
	if err != nil {
		return nil, err
	}
	return &ClaudeReceiver{Reporter: reporter, cfg: cfg}, nil
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
				if err := c.Record(*event); err != nil {
					if errors.Is(err, ErrUsageConflict) {
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

func RunClaudeReceiver(ctx context.Context, cfg Config, diagnostics io.Writer) error {
	address, err := claudeListenAddress(cfg)
	if err != nil {
		return err
	}
	collector, err := NewClaudeReceiver(cfg)
	if err != nil {
		return err
	}
	if err = collector.CheckStorage(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: collector, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	observeHealthy(ctx)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		collector.RunUploads(workerCtx, diagnostics)
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
