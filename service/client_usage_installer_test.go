package service

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInteractiveClientInstallerFetchesOnlySelectedSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX script execution requires sh")
	}
	setupReporterUpdateTables(t)
	f := createSignedReleaseFixture(t)
	require.NoError(t, ActivateReporterRelease(f.catalog.ReleaseID, 17))
	for _, managed := range []bool{false, true} {
		for _, tt := range []struct {
			name, input                  string
			available                    ClientInstallTokens
			codex, claude, fetched, fail bool
		}{
			{name: "codex", input: "y\nn\n", available: ClientInstallTokens{Codex: "codex-token", Claude: "claude-token"}, codex: true, fetched: true},
			{name: "claude", input: "n\ny\n", available: ClientInstallTokens{Codex: "codex-token", Claude: "claude-token"}, claude: true, fetched: true},
			{name: "both", input: "yes\nY\n", available: ClientInstallTokens{Codex: "codex-token", Claude: "claude-token"}, codex: true, claude: true, fetched: true},
			{name: "skip", input: "n\n\n", available: ClientInstallTokens{Codex: "codex-token", Claude: "claude-token"}},
			{name: "invalid_then_retry", input: "all\ny\nn\n", available: ClientInstallTokens{Codex: "codex-token", Claude: "claude-token"}, codex: true, fetched: true},
			{name: "eof", input: "y\n", available: ClientInstallTokens{Codex: "codex-token", Claude: "claude-token"}, fail: true},
			{name: "only_allowed_source", input: "y\n", available: ClientInstallTokens{Claude: "claude-token"}, claude: true, fetched: true},
		} {
			t.Run(fmt.Sprintf("managed=%t/%s", managed, tt.name), func(t *testing.T) {
				requests := make(chan url.Values, 2)
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests <- r.URL.Query()
					_, _ = w.Write([]byte("printf 'selected installer executed\\n'\n"))
				}))
				defer server.Close()
				cert := filepath.Join(t.TempDir(), "ca.pem")
				require.NoError(t, os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600))
				script, err := BuildClientInstallScript(BuildInstallScriptParams{Interactive: true, Managed: managed, AutoUpdate: false, Platform: "macos", ServiceURL: server.URL}, tt.available)
				require.NoError(t, err)
				runner := exec.Command("sh", "-c", script)
				runner.Stdin = strings.NewReader(tt.input)
				runner.Env = append(os.Environ(), "CURL_CA_BUNDLE="+cert)
				output, err := runner.CombinedOutput()
				if tt.fail {
					require.Error(t, err, string(output))
				} else {
					require.NoError(t, err, string(output))
				}
				assert.Equal(t, tt.fetched, strings.Contains(string(output), "selected installer executed"))
				if !tt.fetched {
					assert.Empty(t, requests, "cancellation must not fetch or run an installer")
					return
				}
				require.Len(t, requests, 1)
				query := <-requests
				assert.Equal(t, tt.codex, query.Get("codex_token") != "")
				assert.Equal(t, tt.claude, query.Get("claude_token") != "")
				assert.Equal(t, managed, query.Get("auto_update") == "false", "preserve signed mode even with automatic checks disabled")
				assert.Empty(t, query.Get("interactive"), "selection must not recurse")
			})
		}
	}
}

func TestClientInstallerRejectsMixedAccountsConsumedTokensAndDisabledSources(t *testing.T) {
	setupExternalUsageTestTables(t)
	cfg, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex, model.ExternalUsageSourceClaudeCode}
	_, codex, err := IssueCodexInstallToken(17)
	require.NoError(t, err)
	_, wrongUser, err := IssueClaudeInstallToken(18, "https://api.anthropic.com", false)
	require.NoError(t, err)
	assert.Error(t, ValidateClientInstallTokens(ClientInstallTokens{Codex: codex, Claude: wrongUser}))
	_, claude, err := IssueClaudeInstallToken(17, "https://api.anthropic.com", false)
	require.NoError(t, err)
	tokens := ClientInstallTokens{Codex: codex, Claude: claude}
	require.NoError(t, ValidateClientInstallTokens(tokens))
	assert.Error(t, ValidateClientInstallTokens(ClientInstallTokens{Codex: claude, Claude: codex}), "source-bound tokens cannot be swapped")
	assert.Error(t, ValidateClientInstallTokens(ClientInstallTokens{}))
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex}
	assert.Error(t, ValidateClientInstallTokens(tokens))
	require.NoError(t, ValidateClientInstallTokens(ClientInstallTokens{Codex: codex}))
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex, model.ExternalUsageSourceClaudeCode}
	_, err = ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{InstallToken: codex, DeviceFingerprint: "device"})
	require.NoError(t, err)
	assert.ErrorIs(t, ValidateClientInstallTokens(tokens), model.ErrExternalUsageInstallTokenConsumed)
}

func TestClientInstallerDownloadsOnceAndPreservesWindowsSilentExecution(t *testing.T) {
	for _, platform := range []string{ExternalUsageInstallerPlatformMacOS, ExternalUsageInstallerPlatformWindows} {
		for _, tokens := range []ClientInstallTokens{{Codex: "codex", Claude: "claude"}, {Codex: "codex"}, {Claude: "claude"}} {
			t.Run(platform+tokens.Codex+tokens.Claude, func(t *testing.T) {
				params := BuildInstallScriptParams{Platform: platform, ServiceURL: "https://token.example", AcceptWindowDays: 90}
				script, err := BuildClientInstallScript(params, tokens)
				require.NoError(t, err)
				if platform == ExternalUsageInstallerPlatformMacOS {
					assert.Equal(t, 1, strings.Count(script, `curl -fsSL "$CLIENT_BUNDLE_SERVER/api/external-usage/reporter/binary/`), "the combined installation downloads only one binary")
					assert.Contains(t, script, `trap 'rm -rf "$CLIENT_BUNDLE_DIR"'`)
				} else {
					assert.Equal(t, 1, strings.Count(script, "Invoke-WebRequest"))
					assert.NotContains(t, script, "powershell.exe")
					assert.Contains(t, script, "--log-file")
					assert.Contains(t, script, "-Wait -PassThru")
					assert.Contains(t, script, "finally {\nRemove-Item -LiteralPath $ClientBundleDir")
				}
				assert.Equal(t, tokens.Codex != "", strings.Contains(script, "pro.newapi.usage-reporter.codex") || strings.Contains(script, "new-api-usage-reporter-codex"))
				assert.Equal(t, tokens.Claude != "", strings.Contains(script, "--install-claude"))
				if tokens.Codex == "" {
					assert.Contains(t, script, "?source=claude_code", "Claude-only sites must not need Codex download permission")
				}
				command, err := BuildClientInstallerCommand(params, tokens)
				require.NoError(t, err)
				if platform == ExternalUsageInstallerPlatformWindows {
					encoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, "powershell -ExecutionPolicy Bypass -EncodedCommand "))
					require.NoError(t, err)
					units := make([]uint16, len(encoded)/2)
					for i := range units {
						units[i] = binary.LittleEndian.Uint16(encoded[i*2:])
					}
					command = string(utf16.Decode(units))
				}
				assert.Contains(t, command, "install-clients.")
				assert.Equal(t, tokens.Codex != "", strings.Contains(command, "codex_token="))
				assert.Equal(t, tokens.Claude != "", strings.Contains(command, "claude_token="))
			})
		}
	}
}
