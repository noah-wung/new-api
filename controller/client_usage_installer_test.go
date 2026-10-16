package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
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

func TestClientInstallerOneCommandInstallsBothAndReportsPartialFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installation integration; Windows script and PE contracts are tested separately")
	}
	setupExternalUsageInstallerControllerTestDB(t)
	cfg, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled = true
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex, model.ExternalUsageSourceClaudeCode}
	cfg.MaxDevicesPerUser = 10
	cfg.AcceptWindowDays = 90
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	binary := filepath.Join(assets, "darwin-arm64", "usage-reporter")
	require.NoError(t, os.MkdirAll(filepath.Dir(binary), 0o700))
	build := exec.Command("go", "build", "-o", binary, "./cmd/usage-reporter")
	build.Dir = ".."
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Setenv("NEW_API_USAGE_REPORTER_ASSET_DIR", assets)
	api := gin.New()
	api.POST("/api/external-usage/self/client-installer-command", func(c *gin.Context) {
		c.Set("id", 7)
		CreateExternalUsageSelfClientInstallerCommand(c)
	})
	api.POST("/api/external-usage/self/codex-installer/exchange", ExchangeExternalUsageSelfCodexInstallerToken)
	api.POST("/api/external-usage/self/claude-code-installer/exchange", ExchangeExternalUsageSelfClaudeInstallerToken)
	api.GET("/api/external-usage/reporter/:script", GetExternalUsageReporterInstallScript)
	var downloads atomic.Int32
	api.GET("/api/external-usage/reporter/binary/:target/:name", func(c *gin.Context) {
		downloads.Add(1)
		GetExternalUsageReporterBinary(c)
	})
	server := httptest.NewServer(api)
	defer server.Close()
	home := filepath.Join(root, "home with spaces")
	claudeDir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0o700))
	stubs := filepath.Join(root, "stubs")
	require.NoError(t, os.MkdirAll(stubs, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stubs, "uname"), []byte("#!/bin/sh\necho arm64\n"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stubs, "launchctl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$STUB_LOG\"\n"), 0o700))
	claudeConfigPath := filepath.Join(home, ".new-api-usage-reporter", "claude", "config.json")
	codexConfigPath := filepath.Join(home, ".new-api-usage-reporter", "config.json")
	var previousClaude *usagereporter.Config
	for attempt := 0; attempt < 3; attempt++ {
		request := httptest.NewRequest(http.MethodPost, server.URL+"/api/external-usage/self/client-installer-command?user_id=99", strings.NewReader(`{"platform":"macos","sources":["codex","claude_code"]}`))
		w := httptest.NewRecorder()
		api.ServeHTTP(w, request)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Command string `json:"command"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		require.True(t, result.Success, w.Body.String())
		start := strings.Index(result.Data.Command, "http://")
		require.Positive(t, start)
		end := strings.Index(result.Data.Command[start:], "'")
		require.Positive(t, end)
		scriptURL := result.Data.Command[start : start+end]
		parsed, err := url.Parse(scriptURL)
		require.NoError(t, err)
		for _, query := range []string{"codex_token", "claude_token"} {
			require.NotEmpty(t, parsed.Query().Get(query))
		}
		resp, err := http.Get(scriptURL)
		require.NoError(t, err)
		assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
		var script bytes.Buffer
		_, err = script.ReadFrom(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		if attempt == 2 {
			require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"otelHeadersHelper":"company-helper"}`), 0o600))
		}
		runner := exec.Command("sh")
		runner.Stdin = bytes.NewReader(script.Bytes())
		runner.Env = append(os.Environ(), "HOME="+home, "CLAUDE_CONFIG_DIR="+claudeDir, "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_LOG="+filepath.Join(root, "launchctl.log"), "ANTHROPIC_BASE_URL=", "CLAUDE_CODE_USE_BEDROCK=", "CLAUDE_CODE_USE_VERTEX=", "CLAUDE_CODE_USE_FOUNDRY=")
		output, err := runner.CombinedOutput()
		if attempt == 2 {
			require.Error(t, err, "partial failure must not look like a successful installation")
			assert.Contains(t, string(output), "Codex installation completed")
			assert.Contains(t, string(output), "Claude Code installation failed")
		} else {
			require.NoError(t, err, string(output))
			assert.Contains(t, string(output), "Codex installation completed")
			assert.Contains(t, string(output), "Claude Code installation completed")
		}
		assert.Equal(t, int32(attempt+1), downloads.Load(), "each installation downloads exactly one executable")
		codex, err := usagereporter.LoadConfig(codexConfigPath)
		require.NoError(t, err)
		assert.Equal(t, usagereporter.SourceCodex, codex.Source)
		if attempt == 0 {
			codex.GatewayURLs = []string{"https://gateway-alias.example"}
			codex.ExcludedCodexProviders = []string{"legacy-gateway"}
			require.NoError(t, usagereporter.SaveConfig(codexConfigPath, *codex))
		} else {
			assert.Equal(t, []string{"https://gateway-alias.example"}, codex.GatewayURLs)
			assert.Equal(t, []string{"legacy-gateway"}, codex.ExcludedCodexProviders)
		}
		claude, err := usagereporter.LoadConfig(claudeConfigPath)
		require.NoError(t, err)
		assert.Equal(t, usagereporter.SourceClaudeCode, claude.Source)
		if previousClaude != nil {
			assert.Equal(t, previousClaude.ClaudeReceiverToken, claude.ClaudeReceiverToken)
			assert.Equal(t, previousClaude.Credential, claude.Credential)
		}
		previousClaude = claude
	}
	devices, err := model.ListExternalUsageDevicesByUser(7)
	require.NoError(t, err)
	assert.Len(t, devices, 2, "source-specific devices should be reused on reinstall")
	otherDevices, err := model.ListExternalUsageDevicesByUser(99)
	require.NoError(t, err)
	assert.Empty(t, otherDevices, "query user_id must not override the logged-in account")
}

func TestClientInstallerValidatesSelectionBeforeIssuingTokens(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	cfg, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled = true
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex}
	for _, body := range []string{
		`{"platform":"macos","sources":[]}`,
		`{"platform":"macos","sources":["cursor"]}`,
		`{"platform":"macos","sources":["codex","codex"]}`,
		`{"platform":"macos","sources":["codex","claude_code"],"upstream_url":"https://api.anthropic.com"}`,
	} {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Set("id", 7)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		CreateExternalUsageSelfClientInstallerCommand(ctx)
		var result struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		assert.False(t, result.Success)
	}
	var issued int64
	require.NoError(t, model.DB.Model(&model.ExternalUsageInstallToken{}).Count(&issued).Error)
	assert.Zero(t, issued, "invalid or disabled selections must not issue any source token")
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("id", 7)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"platform":"macos","sources":["codex"]}`))
	CreateExternalUsageSelfClientInstallerCommand(ctx)
	assert.Contains(t, w.Body.String(), `"success":true`, "Codex-only users do not need a Claude route")
}

func TestClientInstallerManualModeNeedsNoSigningSetup(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)
	t.Setenv("NEW_API_USAGE_REPORTER_TRUST_KEYS", "")
	t.Setenv("NEW_API_USAGE_REPORTER_RELEASE_DIR", "")
	for _, automatic := range []any{nil, false, true} {
		body := map[string]any{"platform": "macos", "sources": []string{"codex"}}
		if automatic != nil {
			body["auto_update"] = automatic
		}
		ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "http://gateway.example/api/external-usage/self/client-installer-command", body, 7)
		CreateExternalUsageSelfClientInstallerCommand(ctx)
		response := decodeAPIResponse(t, recorder)
		if automatic == true {
			assert.False(t, response.Success)
			assert.Contains(t, response.Message, "Automatic upgrades are disabled")
			continue
		}
		require.True(t, response.Success, response.Message)
		var data struct {
			Command string `json:"command"`
		}
		require.NoError(t, common.Unmarshal(response.Data, &data))
		assert.Contains(t, data.Command, "http://gateway.example/")
		assert.NotContains(t, data.Command, "auto_update")
	}
}
