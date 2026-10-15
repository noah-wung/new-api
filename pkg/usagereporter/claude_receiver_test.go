package usagereporter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeReceiverConfig(t *testing.T, server string) Config {
	t.Helper()
	return Config{Server: server, Source: SourceClaudeCode, Credential: "reporting-fixture", StatePath: filepath.Join(t.TempDir(), "state.json"), ClaudeReceiverToken: strings.Repeat("a", 64), AcceptWindowDays: 90}
}

func claudeExport(t *testing.T, receiver http.Handler, token string, records ...otlpLogRecord) *httptest.ResponseRecorder {
	t.Helper()
	body, err := common.Marshal(map[string]any{"resourceLogs": []any{map[string]any{"scopeLogs": []any{map[string]any{"logRecords": records}}}}})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	receiver.ServeHTTP(w, request)
	return w
}

func TestClaudeReceiverDurabilityPrivacyAndAcknowledgements(t *testing.T) {
	var uploads []ReportPayload
	status := http.StatusServiceUnavailable
	ackMode := "partial"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer reporting-fixture", r.Header.Get("Authorization"))
		assert.Equal(t, "/api/external-usage/report", r.URL.Path)
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		uploads = append(uploads, payload)
		w.WriteHeader(status)
		if status != http.StatusOK {
			return
		}
		ack := ReportAcknowledgement{AcceptedEventIDs: []string{payload.Events[0].EventID}}
		if ackMode == "all" {
			ack = ReportAcknowledgement{DuplicateEventIDs: []string{payload.Events[0].EventID}, Rejected: []RejectedEvent{{EventID: payload.Events[1].EventID, Reason: "do not store this remote text"}}}
		}
		body, err := common.Marshal(map[string]any{"success": true, "data": ack})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	cfg := claudeReceiverConfig(t, server.URL)
	receiver, err := NewClaudeReceiver(cfg)
	require.NoError(t, err)
	records := []otlpLogRecord{
		claudeTestRecord(t, map[string]any{"request_id": "cli-1", "prompt": "private-prompt", "user.email": "private-email"}),
		claudeTestRecord(t, map[string]any{"request_id": "vscode-2", "app.entrypoint": "claude-vscode"}),
		claudeTestRecord(t, map[string]any{"request_id": "subagent-3", "session.id": "subagent-session"}),
		claudeTestRecord(t, map[string]any{ClaudeUpstreamAttribute: server.URL, "request_id": "gateway"}),
		claudeTestRecord(t, map[string]any{ClaudeUpstreamAttribute: nil}),
		{Body: otlpValue{StringValue: "claude_code.user_prompt"}, Attributes: []otlpAttribute{{Key: "prompt", Value: otlpValue{StringValue: "private-body"}}}},
	}
	response := claudeExport(t, receiver, cfg.ClaudeReceiverToken, records...)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"rejectedLogRecords":"1"`)
	files, err := os.ReadDir(receiver.queueDir)
	require.NoError(t, err)
	require.Len(t, files, 3, "gateway and unrelated records cannot enter the queue")
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(receiver.queueDir, file.Name()))
		require.NoError(t, err)
		for _, private := range []string{"private-prompt", "private-email", "private-body", "reporting-fixture", "session-1", "subagent-session", cfg.ClaudeReceiverToken} {
			assert.NotContains(t, string(content), private)
		}
	}
	assert.Error(t, receiver.Flush())
	// Restart before retry: queue contents and identities must survive.
	restarted, err := NewClaudeReceiver(cfg)
	require.NoError(t, err)
	status = http.StatusOK
	require.NoError(t, restarted.Flush())
	require.Len(t, uploads, 2)
	assert.Equal(t, uploads[0].Events, uploads[1].Events)
	assert.Equal(t, SourceClaudeCode, uploads[1].Source)
	files, err = os.ReadDir(receiver.queueDir)
	require.NoError(t, err)
	assert.Len(t, files, 2, "unacknowledged records remain queued")
	ackMode = "all"
	require.NoError(t, restarted.Flush())
	files, err = os.ReadDir(receiver.queueDir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.True(t, strings.HasSuffix(files[0].Name(), ".rejected"))
	content, err := os.ReadFile(filepath.Join(receiver.queueDir, files[0].Name()))
	require.NoError(t, err)
	assert.NotContains(t, string(content), "do not store this remote text")
	require.NoError(t, restarted.Flush())
	assert.Len(t, uploads, 3, "permanent rejections must not create an endless retry loop")
}

func TestClaudeReceiverRejectsUnauthorizedAndMalformedExports(t *testing.T) {
	cfg := claudeReceiverConfig(t, "https://token.example")
	receiver, err := NewClaudeReceiver(cfg)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, method, contentType, body, token, origin string
		status                                         int
	}{
		{name: "no credential", method: "POST", contentType: "application/json", body: "{}", status: 401},
		{name: "browser origin", method: "POST", contentType: "application/json", body: "{}", token: cfg.ClaudeReceiverToken, origin: "https://website.example", status: 401},
		{name: "protobuf", method: "POST", contentType: "application/x-protobuf", body: "{}", token: cfg.ClaudeReceiverToken, status: 415},
		{name: "invalid JSON", method: "POST", contentType: "application/json", body: "{", token: cfg.ClaudeReceiverToken, status: 400},
		{name: "trailing JSON", method: "POST", contentType: "application/json", body: "{}{}", token: cfg.ClaudeReceiverToken, status: 400},
		{name: "get", method: "GET", status: 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/v1/logs", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer "+tc.token)
			r.Header.Set("Content-Type", tc.contentType)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			receiver.ServeHTTP(w, r)
			assert.Equal(t, tc.status, w.Code)
		})
	}
	files, err := os.ReadDir(receiver.queueDir)
	require.NoError(t, err)
	assert.Empty(t, files)
	for _, address := range []string{"0.0.0.0:4318", ":4318", "localhost:4318", "192.168.1.1:4318", "127.0.0.1:0"} {
		cfg.ClaudeListen = address
		_, err := NewClaudeReceiver(cfg)
		assert.Error(t, err, address)
	}
}

func TestClaudeReceiverDeduplicatesAndRechecksGatewayPolicy(t *testing.T) {
	cfg := claudeReceiverConfig(t, "https://token.example")
	receiver, err := NewClaudeReceiver(cfg)
	require.NoError(t, err)
	record := claudeTestRecord(t, nil)
	for i := 0; i < 2; i++ {
		response := claudeExport(t, receiver, cfg.ClaudeReceiverToken, record)
		assert.Equal(t, "{}", response.Body.String())
	}
	files, err := os.ReadDir(receiver.queueDir)
	require.NoError(t, err)
	assert.Len(t, files, 1)
	conflicting := record
	conflicting.Attributes = append(append([]otlpAttribute{}, record.Attributes...), otlpAttribute{Key: "output_tokens", Value: otlpValue{StringValue: "41"}})
	response := claudeExport(t, receiver, cfg.ClaudeReceiverToken, conflicting)
	assert.Contains(t, response.Body.String(), `"rejectedLogRecords":"1"`)
	cfg.GatewayURLs = []string{"https://api.anthropic.com"}
	restarted, err := NewClaudeReceiver(cfg)
	require.NoError(t, err)
	require.NoError(t, restarted.Flush(), "a newly excluded route is removed without contacting the server")
	files, err = os.ReadDir(receiver.queueDir)
	require.NoError(t, err)
	assert.Empty(t, files)
	otherIdentity := cfg
	otherIdentity.Credential = "another-account"
	other, err := NewClaudeReceiver(otherIdentity)
	require.NoError(t, err)
	assert.NotEqual(t, restarted.queueDir, other.queueDir)
}

func TestClaudeReceiverShutdown(t *testing.T) {
	// Reserve an available port and use a diagnostics notification for readiness,
	// avoiding sleeps or request polling in this lifecycle test.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	cfg := claudeReceiverConfig(t, "https://token.example")
	cfg.ClaudeListen = address
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	finished := make(chan error, 1)
	go func() { finished <- RunClaudeReceiver(ctx, cfg, writer) }()
	ready := make(chan string, 1)
	go func() {
		buf := make([]byte, 1024)
		n, _ := reader.Read(buf)
		ready <- string(buf[:n])
	}()
	select {
	case text := <-ready:
		assert.Contains(t, text, "listening on "+address)
	case <-time.After(5 * time.Second):
		t.Fatal("receiver failed to start")
	}
	cancel()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal(fmt.Sprintf("receiver failed to stop at %s", address))
	}
}

// The in-memory transport keeps the schedule test independent of network I/O
// so synctest can advance five-minute intervals without wall-clock sleeps.
type claudeReportTransport struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (transport claudeReportTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport.roundTrip(request)
}

func TestClaudeUploadsEveryFiveMinutesAndRetriesWithoutRecounting(t *testing.T) {
	cfg := claudeReceiverConfig(t, "https://token.example")
	receiver, err := NewClaudeReceiver(cfg)
	require.NoError(t, err)
	response := claudeExport(t, receiver, cfg.ClaudeReceiverToken, claudeTestRecord(t, map[string]any{"request_id": "first"}))
	require.Equal(t, http.StatusOK, response.Code)
	var uploads []ReportPayload
	previousTransport := http.DefaultTransport
	http.DefaultTransport = claudeReportTransport{roundTrip: func(request *http.Request) (*http.Response, error) {
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		uploads = append(uploads, payload)
		status := http.StatusOK
		if len(uploads) == 1 {
			status = http.StatusServiceUnavailable
		}
		ids := make([]string, 0, len(payload.Events))
		for _, event := range payload.Events {
			ids = append(ids, event.EventID)
		}
		body, err := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		require.NoError(t, err)
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	}}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var diagnostics bytes.Buffer
		go receiver.runUploads(ctx, &diagnostics)
		synctest.Wait()
		require.Len(t, uploads, 1, "pending events are attempted once at startup")
		assert.Contains(t, diagnostics.String(), "queued events retained")
		time.Sleep(5*time.Minute - time.Nanosecond)
		synctest.Wait()
		require.Len(t, uploads, 1, "failed uploads wait until the next reporting interval")
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Len(t, uploads, 2)
		assert.Equal(t, uploads[0].Events, uploads[1].Events, "retries retain event identities")
		response := claudeExport(t, receiver, cfg.ClaudeReceiverToken, claudeTestRecord(t, map[string]any{"request_id": "second"}))
		require.Equal(t, http.StatusOK, response.Code)
		time.Sleep(5*time.Minute - time.Nanosecond)
		synctest.Wait()
		require.Len(t, uploads, 2, "new events stay local until the next five-minute tick")
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Len(t, uploads, 3)
		require.Len(t, uploads[2].Events, 1)
		assert.NotEqual(t, uploads[0].Events[0].EventID, uploads[2].Events[0].EventID)
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assert.Len(t, uploads, 3, "empty queues must not send HTTP requests")
		cancel()
		synctest.Wait()
	})
}

func TestClaudeFlushDrainsSnapshotInBatchesAndPreservesUnacknowledgedEvents(t *testing.T) {
	var uploads []ReportPayload
	var addedDuringUpload bool
	var receiver *ClaudeReceiver
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		uploads = append(uploads, payload)
		assert.LessOrEqual(t, len(payload.Events), 100)
		ids := make([]string, 0, len(payload.Events))
		for _, event := range payload.Events {
			ids = append(ids, event.EventID)
		}
		if len(uploads) == 1 {
			// Incomplete ACKs are retried next cycle, not repeatedly in this one.
			ids = ids[1:]
			response := claudeExport(t, receiver, receiver.cfg.ClaudeReceiverToken, claudeTestRecord(t, map[string]any{"request_id": "arrived-during-upload"}))
			require.Equal(t, http.StatusOK, response.Code)
			addedDuringUpload = true
		}
		body, err := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	cfg := claudeReceiverConfig(t, server.URL)
	var err error
	receiver, err = NewClaudeReceiver(cfg)
	require.NoError(t, err)
	for i := 0; i < 205; i++ {
		response := claudeExport(t, receiver, cfg.ClaudeReceiverToken, claudeTestRecord(t, map[string]any{"request_id": fmt.Sprintf("request-%d", i)}))
		require.Equal(t, http.StatusOK, response.Code)
	}
	require.NoError(t, receiver.Flush())
	require.Len(t, uploads, 3, "one reporting cycle handles more than 100 accumulated requests")
	assert.Len(t, uploads[0].Events, 100)
	assert.Len(t, uploads[1].Events, 100)
	assert.Len(t, uploads[2].Events, 5)
	assert.True(t, addedDuringUpload)
	files, err := os.ReadDir(receiver.queueDir)
	require.NoError(t, err)
	assert.Len(t, files, 2, "retain the unacknowledged event and the newly arrived event")
	require.NoError(t, receiver.Flush())
	require.Len(t, uploads, 4)
	assert.Len(t, uploads[3].Events, 2)
	files, err = os.ReadDir(receiver.queueDir)
	require.NoError(t, err)
	assert.Empty(t, files)
}
