package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeInstallerCommandDownloadsAndReinstallsWithoutReplacingCodex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installer lifecycle; Windows binaries are cross-built separately")
	}
	setupExternalUsageInstallerControllerTestDB(t)
	cfg, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled = true
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex, model.ExternalUsageSourceClaudeCode}
	cfg.AcceptWindowDays = 90
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	assetPath := filepath.Join(assets, "darwin-arm64", "usage-reporter")
	require.NoError(t, os.MkdirAll(filepath.Dir(assetPath), 0o700))
	build := exec.Command("go", "build", "-o", assetPath, "./cmd/usage-reporter")
	build.Dir = ".."
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Setenv("NEW_API_USAGE_REPORTER_ASSET_DIR", assets)
	api := gin.New()
	api.POST("/api/external-usage/self/claude-code-installer-command", func(c *gin.Context) { c.Set("id", 7); CreateExternalUsageSelfClaudeInstallerCommand(c) })
	api.POST("/api/external-usage/self/claude-code-installer/exchange", ExchangeExternalUsageSelfClaudeInstallerToken)
	api.GET("/api/external-usage/reporter/:script", GetExternalUsageReporterInstallScript)
	api.GET("/api/external-usage/reporter/binary/:target/:name", GetExternalUsageReporterBinary)
	server := httptest.NewServer(api)
	defer server.Close()
	home := filepath.Join(root, "home with &' chars")
	claudeDir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"permissions":{"deny":["Bash(rm:*)"]}}`), 0o600))
	codexPath := filepath.Join(home, ".new-api-usage-reporter", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(codexPath), 0o700))
	require.NoError(t, os.WriteFile(codexPath, []byte("codex configuration must remain intact"), 0o600))
	stubDir := filepath.Join(root, "stubs")
	require.NoError(t, os.MkdirAll(stubDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stubDir, "uname"), []byte("#!/bin/sh\nprintf 'arm64\\n'\n"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stubDir, "launchctl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$STUB_LOG\"\n"), 0o700))
	var firstConfig *usagereporter.Config
	for attempt := 0; attempt < 3; attempt++ {
		body := `{"platform":"macos","upstream_url":"https://API.anthropic.com:443/v1","upstream_is_gateway":false}`
		resp, err := http.Post(server.URL+"/api/external-usage/self/claude-code-installer-command?user_id=99", "application/json", strings.NewReader(body))
		require.NoError(t, err)
		var commandResponse struct {
			Success bool `json:"success"`
			Data    struct {
				Command string `json:"command"`
			} `json:"data"`
		}
		require.NoError(t, common.DecodeJson(resp.Body, &commandResponse))
		resp.Body.Close()
		require.True(t, commandResponse.Success)
		start := strings.Index(commandResponse.Data.Command, "http://")
		require.Positive(t, start)
		end := strings.Index(commandResponse.Data.Command[start:], "'")
		require.Positive(t, end)
		scriptURL := commandResponse.Data.Command[start : start+end]
		parsed, err := url.Parse(scriptURL)
		require.NoError(t, err)
		rawToken := parsed.Query().Get("token")
		digest := sha256.Sum256([]byte(rawToken))
		token, err := model.GetExternalUsageInstallTokenByHash(model.DB, hex.EncodeToString(digest[:]))
		require.NoError(t, err)
		assert.Equal(t, 7, token.UserID, "query user_id cannot override the authenticated user")
		assert.Equal(t, "https://api.anthropic.com", token.UpstreamURL)
		wrongResp, err := http.Get(server.URL + "/api/external-usage/reporter/install.sh?token=" + url.QueryEscape(rawToken))
		require.NoError(t, err)
		var wrong struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.DecodeJson(wrongResp.Body, &wrong))
		wrongResp.Body.Close()
		assert.False(t, wrong.Success, "Claude tokens cannot download a Codex installer")
		script, err := http.Get(scriptURL)
		require.NoError(t, err)
		scriptBytes, err := io.ReadAll(script.Body)
		script.Body.Close()
		require.NoError(t, err)
		runner := exec.Command("sh")
		runner.Stdin = bytes.NewReader(scriptBytes)
		runner.Env = append(os.Environ(), "HOME="+home, "CLAUDE_CONFIG_DIR="+claudeDir, "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_LOG="+filepath.Join(root, "launchctl.log"), "ANTHROPIC_BASE_URL=", "CLAUDE_CODE_USE_BEDROCK=", "CLAUDE_CODE_USE_VERTEX=", "CLAUDE_CODE_USE_FOUNDRY=")
		var priorServiceCalls []byte
		mismatchedSettings := []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://gateway.other.example"}}`)
		if attempt == 2 {
			priorServiceCalls, err = os.ReadFile(filepath.Join(root, "launchctl.log"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), mismatchedSettings, 0o600))
		}
		output, err := runner.CombinedOutput()
		if attempt == 2 {
			require.Error(t, err, "a route mismatch must stop installation")
			assert.Contains(t, string(output), "declared upstream differs")
			serviceCalls, err := os.ReadFile(filepath.Join(root, "launchctl.log"))
			require.NoError(t, err)
			assert.Equal(t, priorServiceCalls, serviceCalls, "failed configuration cannot stop or replace the existing service")
			settings, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
			require.NoError(t, err)
			assert.Equal(t, mismatchedSettings, settings)
			continue
		}
		require.NoError(t, err, string(output))
		assert.Contains(t, string(output), "Claude Code usage reporter installed")
		installed, err := usagereporter.LoadConfig(filepath.Join(home, ".new-api-usage-reporter", "claude", "config.json"))
		require.NoError(t, err)
		assert.Equal(t, usagereporter.SourceClaudeCode, installed.Source)
		assert.Equal(t, 90, installed.AcceptWindowDays)
		if firstConfig == nil {
			firstConfig = installed
			require.NoError(t, os.WriteFile(installed.StatePath, []byte("preserved client state"), 0o600))
		} else {
			assert.Equal(t, firstConfig.Credential, installed.Credential)
			assert.Equal(t, firstConfig.ClaudeReceiverToken, installed.ClaudeReceiverToken)
			state, err := os.ReadFile(installed.StatePath)
			require.NoError(t, err)
			assert.Equal(t, "preserved client state", string(state))
		}
		var reusedToken model.ExternalUsageInstallToken
		require.NoError(t, model.DB.First(&reusedToken, token.Id).Error)
		assert.Equal(t, model.ExternalUsageStatusConsumed, reusedToken.Status)
	}
	codex, err := os.ReadFile(codexPath)
	require.NoError(t, err)
	assert.Equal(t, "codex configuration must remain intact", string(codex))
	var devices int64
	require.NoError(t, model.DB.Model(&model.ExternalUsageDevice{}).Count(&devices).Error)
	assert.Equal(t, int64(1), devices)
	plist, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", "pro.newapi.usage-reporter.claude.plist"))
	require.NoError(t, err)
	var document struct {
		XMLName xml.Name `xml:"plist"`
	}
	require.NoError(t, xml.Unmarshal(plist, &document), "special characters in HOME must be escaped in the LaunchAgent")
	assert.Contains(t, string(plist), "<string>--serve</string>")
	assert.Contains(t, string(plist), "<key>KeepAlive</key><true/>")
}
