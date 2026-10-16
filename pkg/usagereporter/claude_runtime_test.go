package usagereporter

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Optional contract check against a real Claude binary. All model traffic goes
// to a loopback fixture, with a private HOME and no inherited API credentials.
func TestClaudeRuntimeTelemetryContract(t *testing.T) {
	claude := os.Getenv("NEW_API_CLAUDE_TEST_BINARY")
	if claude == "" {
		t.Skip("set NEW_API_CLAUDE_TEST_BINARY to validate an installed Claude Code CLI")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "helper with '&$ chars", "usage-reporter")
	require.NoError(t, os.MkdirAll(filepath.Dir(binary), 0o700))
	build := exec.Command("go", "build", "-o", binary, "./cmd/usage-reporter")
	build.Dir = "../.."
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	for _, route := range []string{"user external", "project gateway", "environment alias"} {
		t.Run(route, func(t *testing.T) {
			var requests atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.URL.Path != "/v1/messages" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				requests.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("request-id", "req-runtime-contract")
				for _, event := range []struct{ name, data string }{
					{"message_start", `{"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"usage":{"input_tokens":100,"cache_read_input_tokens":200,"cache_creation_input_tokens":300,"output_tokens":0}}}`},
					{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
					{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}`},
					{"content_block_stop", `{"type":"content_block_stop","index":0}`},
					{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":40}}`},
					{"message_stop", `{"type":"message_stop"}`},
				} {
					_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, event.data)
				}
			}))
			defer api.Close()
			cfg := claudeReceiverConfig(t, "https://gateway.invalid")
			cfg.ClaudeAutoRoute = true
			if route == "project gateway" {
				cfg.Server = api.URL
			}
			if route == "environment alias" {
				cfg.GatewayURLs = []string{api.URL}
			}
			receiver, err := NewClaudeReceiver(cfg)
			require.NoError(t, err)
			var exports atomic.Int32
			telemetry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, api.URL, r.Header.Get(ClaudeOriginHeader), "helper must inherit effective runtime settings")
				exports.Add(1)
				receiver.ServeHTTP(w, r)
			}))
			defer telemetry.Close()
			cfg.ClaudeListen = telemetry.Listener.Addr().(*net.TCPAddr).String()
			home := t.TempDir()
			settingsPath := filepath.Join(home, ".claude", "settings.json")
			require.NoError(t, ConfigureClaudeTelemetry(cfg, settingsPath, binary))
			content, err := os.ReadFile(settingsPath)
			require.NoError(t, err)
			var settings map[string]any
			require.NoError(t, common.Unmarshal(content, &settings))
			env := settings["env"].(map[string]any)
			env["OTEL_LOGS_EXPORT_INTERVAL"] = "50"
			if route == "user external" {
				env["ANTHROPIC_BASE_URL"] = api.URL
			}
			if route == "project gateway" {
				// Prevent startup from trying the official managed-settings API
				// before project settings replace this user-level default.
				env["ANTHROPIC_BASE_URL"] = "http://127.0.0.1:1"
			}
			content, err = common.Marshal(settings)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(settingsPath, content, 0o600))
			if route == "project gateway" {
				projectSettings := filepath.Join(home, "project", ".claude")
				require.NoError(t, os.MkdirAll(projectSettings, 0o700))
				content, err = common.Marshal(map[string]any{"env": map[string]string{"ANTHROPIC_BASE_URL": api.URL}})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(projectSettings, "settings.json"), content, 0o600))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			debugPath := filepath.Join(home, "debug.log")
			cmd := exec.CommandContext(ctx, claude, "-p", "Reply OK", "--model", "claude-sonnet-4-6", "--tools", "", "--max-turns", "1", "--no-session-persistence", "--debug-file", debugPath)
			cmd.Dir = home
			if route == "project gateway" {
				cmd.Dir = filepath.Join(home, "project")
			}
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "CLAUDE_CONFIG_DIR=" + filepath.Dir(settingsPath), "ANTHROPIC_API_KEY=local-fixture-only", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_SKIP_API_KEY_VERIFICATION=1", "HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1", "NO_PROXY=127.0.0.1,localhost"}
			if route == "environment alias" {
				cmd.Env = append(cmd.Env, "ANTHROPIC_BASE_URL="+api.URL)
			}
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			assert.Contains(t, string(output), "OK")
			assert.Positive(t, requests.Load())
			if exports.Load() == 0 {
				debug, err := os.ReadFile(debugPath)
				require.NoError(t, err)
				for _, line := range strings.Split(string(debug), "\n") {
					if strings.Contains(line, "[3P telemetry]") || strings.Contains(line, "otelHeadersHelper") {
						t.Log(line)
					}
				}
			}
			assert.Positive(t, exports.Load(), "real CLI must export logs before exit")
			files, err := os.ReadDir(receiver.queueDir)
			require.NoError(t, err)
			if route != "user external" {
				assert.Empty(t, files, "gateway model usage must not enter the upload queue")
				return
			}
			require.Len(t, files, 1)
			data, err := os.ReadFile(filepath.Join(receiver.queueDir, files[0].Name()))
			require.NoError(t, err)
			var event CollectedEvent
			require.NoError(t, common.Unmarshal(data, &event))
			assert.Equal(t, 600, event.Event.InputTokens)
			assert.Equal(t, 200, event.Event.CachedInputTokens)
			assert.Equal(t, 40, event.Event.OutputTokens)
			assert.Equal(t, 640, event.Event.TotalTokens)
			assert.NotContains(t, string(data), "Reply OK")
			assert.False(t, strings.Contains(string(data), "local-fixture-only"))
		})
	}
}
