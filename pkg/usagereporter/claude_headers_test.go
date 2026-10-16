package usagereporter

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeRuntimeHeadersDetectEffectiveRouteWithoutCredentials(t *testing.T) {
	for _, key := range []string{"ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "ANTHROPIC_BEDROCK_BASE_URL", "ANTHROPIC_BEDROCK_MANTLE_BASE_URL", "CLAUDE_CODE_USE_VERTEX", "ANTHROPIC_VERTEX_BASE_URL", "CLAUDE_CODE_USE_FOUNDRY", "ANTHROPIC_FOUNDRY_BASE_URL", "ANTHROPIC_FOUNDRY_RESOURCE", "CLAUDE_CODE_USE_ANTHROPIC_AWS", "ANTHROPIC_AWS_BASE_URL"} {
		t.Setenv(key, "")
	}
	t.Setenv("ANTHROPIC_API_KEY", "must-not-export")
	for _, tc := range []struct {
		name   string
		env    map[string]string
		origin string
	}{
		{"subscription or direct", nil, "https://api.anthropic.com"},
		{"third party", map[string]string{"ANTHROPIC_BASE_URL": "https://THIRD.example:443/v1"}, "https://third.example"},
		{"gateway", map[string]string{"ANTHROPIC_BASE_URL": "http://172.16.0.37:3000/v1"}, "http://172.16.0.37:3000"},
		{"invalid", map[string]string{"ANTHROPIC_BASE_URL": "https://secret@private.example"}, "unknown"},
		{"Bedrock", map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"}, "https://bedrock-runtime.amazonaws.com"},
		{"Bedrock gateway override", map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "ANTHROPIC_BEDROCK_BASE_URL": "https://token.example/bedrock"}, "https://token.example"},
		{"Vertex gateway override", map[string]string{"CLAUDE_CODE_USE_VERTEX": "true", "ANTHROPIC_VERTEX_BASE_URL": "https://token.example/vertex"}, "https://token.example"},
		{"Foundry", map[string]string{"CLAUDE_CODE_USE_FOUNDRY": "1", "ANTHROPIC_FOUNDRY_RESOURCE": "test-resource"}, "https://test-resource.services.ai.azure.com"},
		{"Foundry missing route", map[string]string{"CLAUDE_CODE_USE_FOUNDRY": "1"}, "unknown"},
		{"AWS gateway override", map[string]string{"CLAUDE_CODE_USE_ANTHROPIC_AWS": "1", "ANTHROPIC_AWS_BASE_URL": "https://token.example/aws"}, "https://token.example"},
		{"conflicting providers", map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "CLAUDE_CODE_USE_VERTEX": "1"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			assert.Equal(t, map[string]string{ClaudeOriginHeader: tc.origin}, ClaudeTelemetryHeaders())
		})
	}
}

func TestClaudeAutomaticRouteUsesRuntimeHeaderWithGatewayPrecedence(t *testing.T) {
	cfg := claudeReceiverConfig(t, "https://token.example")
	cfg.ClaudeAutoRoute = true
	cfg.GatewayURLs = []string{"http://172.16.0.37:3000", "https://alias.example"}
	// Upgrade from the old official-only allowlist must accept other external routes.
	cfg.ClaudeExternalURLs = []string{"https://api.anthropic.com"}
	receiver, err := NewClaudeReceiver(cfg)
	require.NoError(t, err)
	for _, tc := range []struct {
		origin             string
		accepted, rejected bool
	}{
		{"https://api.anthropic.com", true, false},
		{"https://third.example/v1", true, false},
		{"https://token.example:443/v1", false, false},
		{"http://172.16.0.37:3000/v1", false, false},
		{"https://alias.example/v1", false, false},
		{"", false, true},
		{"unknown", false, true},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			before, err := os.ReadDir(receiver.queueDir)
			require.NoError(t, err)
			// A stale static declaration may never override runtime gateway evidence.
			record := claudeTestRecord(t, map[string]any{"request_id": tc.origin, ClaudeUpstreamAttribute: "https://api.anthropic.com"})
			body, err := common.Marshal(map[string]any{"resourceLogs": []any{map[string]any{"scopeLogs": []any{map[string]any{"logRecords": []otlpLogRecord{record}}}}}})
			require.NoError(t, err)
			r := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+cfg.ClaudeReceiverToken)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set(ClaudeOriginHeader, tc.origin)
			w := httptest.NewRecorder()
			receiver.ServeHTTP(w, r)
			require.Equal(t, http.StatusOK, w.Code)
			after, err := os.ReadDir(receiver.queueDir)
			require.NoError(t, err)
			expected := len(before)
			if tc.accepted {
				expected++
			}
			assert.Len(t, after, expected)
			if tc.rejected {
				assert.Contains(t, w.Body.String(), "rejectedLogRecords")
			} else {
				assert.JSONEq(t, `{}`, w.Body.String())
			}
		})
	}
}
