package usagereporter

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeTestRecord(t *testing.T, values map[string]any) otlpLogRecord {
	t.Helper()
	defaults := map[string]any{
		"event.name": "api_request", "session.id": "session-1", "request_id": "request-1",
		"event.sequence": "1", "model": "claude-sonnet-test", "input_tokens": 100,
		"cache_read_tokens": 200, "cache_creation_tokens": 300, "output_tokens": 40,
		ClaudeUpstreamAttribute: "https://api.anthropic.com", "app.entrypoint": "cli",
	}
	for key, value := range values {
		if value == nil {
			delete(defaults, key)
		} else {
			defaults[key] = value
		}
	}
	attrs := make([]otlpAttribute, 0, len(defaults))
	for key, value := range defaults {
		attrs = append(attrs, otlpAttribute{Key: key, Value: otlpValue{StringValue: fmt.Sprint(value)}})
	}
	return otlpLogRecord{TimeUnixNano: fmt.Sprint(time.Now().Add(-time.Minute).UnixNano()), Attributes: attrs}
}

func TestClaudeRequestTokenAccountingAndRouting(t *testing.T) {
	policy, err := newClaudeRoutePolicy(Config{Server: "https://token.example", GatewayURLs: []string{"http://172.16.0.37:3000"}})
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		values  map[string]any
		wantErr bool
		skipped bool
	}{
		{name: "cli"},
		{name: "vscode", values: map[string]any{"app.entrypoint": "claude-vscode"}},
		{name: "gateway same model", values: map[string]any{ClaudeUpstreamAttribute: "https://TOKEN.example:443/v1"}, skipped: true},
		{name: "gateway alias", values: map[string]any{ClaudeUpstreamAttribute: "http://172.16.0.37:3000/v1"}, skipped: true},
		{name: "missing route", values: map[string]any{ClaudeUpstreamAttribute: nil}, wantErr: true},
		{name: "unknown route", values: map[string]any{ClaudeUpstreamAttribute: "https://proxy.example"}, wantErr: true},
		{name: "lookalike host", values: map[string]any{ClaudeUpstreamAttribute: "https://api.anthropic.com.other.example"}, wantErr: true},
		{name: "different port", values: map[string]any{ClaudeUpstreamAttribute: "https://api.anthropic.com:8443"}, wantErr: true},
		{name: "URL credentials", values: map[string]any{ClaudeUpstreamAttribute: "https://secret@api.anthropic.com"}, wantErr: true},
		{name: "negative count", values: map[string]any{"input_tokens": -1}, wantErr: true},
		{name: "fractional count", values: map[string]any{"output_tokens": "1.5"}, wantErr: true},
		{name: "missing output", values: map[string]any{"output_tokens": nil}, wantErr: true},
		{name: "overflow sum", values: map[string]any{"input_tokens": 2147483647}, wantErr: true},
		{name: "overflow field", values: map[string]any{"input_tokens": "18446744073709551615"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event, err := normalizeClaudeRequest(claudeTestRecord(t, tc.values), nil, policy, buildClientAcceptWindow(90))
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, event)
				return
			}
			require.NoError(t, err)
			if tc.skipped {
				assert.Nil(t, event)
				return
			}
			require.NotNil(t, event)
			assert.Equal(t, 600, event.Event.InputTokens)
			assert.Equal(t, 200, event.Event.CachedInputTokens)
			assert.Equal(t, 40, event.Event.OutputTokens)
			assert.Equal(t, 640, event.Event.TotalTokens)
			assert.Zero(t, event.Event.ReasoningOutputTokens)
			assert.NotEqual(t, "session-1", event.Event.SessionID)
		})
	}
}

func TestClaudeIdentitiesSurviveRetryAndSessionResume(t *testing.T) {
	policy, err := newClaudeRoutePolicy(Config{Server: "https://token.example"})
	require.NoError(t, err)
	record := claudeTestRecord(t, nil)
	first, err := normalizeClaudeRequest(record, nil, policy, clientAcceptWindow{})
	require.NoError(t, err)
	for _, values := range []map[string]any{
		{"event.sequence": "0"}, {"app.entrypoint": "claude-vscode"}, {"session.id": "resumed-session"},
	} {
		replay, err := normalizeClaudeRequest(claudeTestRecord(t, values), nil, policy, clientAcceptWindow{})
		require.NoError(t, err)
		assert.Equal(t, first.Event.EventID, replay.Event.EventID, "server request ID deduplicates independently of entrypoint or session")
	}
	fallback := claudeTestRecord(t, map[string]any{"request_id": nil})
	a, err := normalizeClaudeRequest(fallback, nil, policy, clientAcceptWindow{})
	require.NoError(t, err)
	b, err := normalizeClaudeRequest(fallback, nil, policy, clientAcceptWindow{})
	require.NoError(t, err)
	assert.Equal(t, a.Event.EventID, b.Event.EventID)
	fallback.TimeUnixNano = fmt.Sprint(time.Now().UnixNano())
	c, err := normalizeClaudeRequest(fallback, nil, policy, clientAcceptWindow{})
	require.NoError(t, err)
	assert.NotEqual(t, a.Event.EventID, c.Event.EventID, "sequence reused after resume cannot collapse a later request")
	client := claudeTestRecord(t, map[string]any{"request_id": nil, "client_request_id": "client-uuid"})
	a, err = normalizeClaudeRequest(client, nil, policy, clientAcceptWindow{})
	require.NoError(t, err)
	client.TimeUnixNano = fmt.Sprint(time.Now().UnixNano())
	b, err = normalizeClaudeRequest(client, nil, policy, clientAcceptWindow{})
	require.NoError(t, err)
	assert.Equal(t, a.Event.EventID, b.Event.EventID)
}

func TestClaudeOTLPTypesAndWindow(t *testing.T) {
	policy, err := newClaudeRoutePolicy(Config{Server: "https://token.example"})
	require.NoError(t, err)
	var record otlpLogRecord
	content := fmt.Sprintf(`{"body":{"stringValue":"claude_code.api_request"},"timeUnixNano":"%d","attributes":[{"key":"input_tokens","value":{"intValue":1000000}},{"key":"output_tokens","value":{"doubleValue":40}},{"key":"model","value":{"stringValue":"claude-test"}},{"key":"request_id","value":{"stringValue":"req-types"}}]}`, time.Now().UnixNano())
	require.NoError(t, common.Unmarshal([]byte(content), &record))
	resource := []otlpAttribute{{Key: ClaudeUpstreamAttribute, Value: otlpValue{StringValue: "https://api.anthropic.com"}}}
	event, err := normalizeClaudeRequest(record, resource, policy, buildClientAcceptWindow(90))
	require.NoError(t, err)
	assert.Equal(t, 1000040, event.Event.TotalTokens)
	for _, when := range []time.Time{time.Now().Add(-91 * 24 * time.Hour), time.Now().Add(time.Hour)} {
		record.TimeUnixNano = fmt.Sprint(when.UnixNano())
		_, err := normalizeClaudeRequest(record, resource, policy, buildClientAcceptWindow(90))
		assert.Error(t, err)
	}
	record.TimeUnixNano = "not-a-timestamp"
	_, err = normalizeClaudeRequest(record, resource, policy, buildClientAcceptWindow(90))
	assert.Error(t, err)
}
