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
