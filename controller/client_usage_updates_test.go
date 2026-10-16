package controller

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prepareControllerSignedRelease(t *testing.T) string {
	t.Helper()
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)
	setting, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	setting.AllowedSources = []string{"codex", "claude_code"}
	require.NoError(t, model.DB.AutoMigrate(&model.ReporterDeployment{}, &model.ReporterRelease{}))
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	keys, err := common.Marshal(reporterupdate.TrustKeys{"fixture-key": base64.StdEncoding.EncodeToString(pub)})
	require.NoError(t, err)
	t.Setenv("NEW_API_USAGE_REPORTER_TRUST_KEYS", string(keys))
	root := t.TempDir()
	t.Setenv("NEW_API_USAGE_REPORTER_RELEASE_DIR", root)
	c := reporterupdate.Catalog{SchemaVersion: 1, Sequence: 1, IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(), ReleaseID: "controller-signed", Version: "1.0.3", Commit: "fixture", Protocol: reporterupdate.Range{Min: 1, Max: 1}, Config: reporterupdate.Range{Min: 1, Max: 1}, State: reporterupdate.Range{Min: 2, Max: 2}, Queue: reporterupdate.Range{Min: 1, Max: 1}, RolloutPercent: 10, RolloutSeed: "stable"}
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
		name, err := reporterupdate.TargetName(target)
		require.NoError(t, err)
		data := []byte("fixture-" + target)
		path := filepath.Join(root, c.ReleaseID, target, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, data, 0o700))
		c.Targets = append(c.Targets, reporterupdate.Artifact{Target: target, Path: "/api/external-usage/reporter/releases/" + c.ReleaseID + "/" + target + "/" + name, SHA256: reporterupdate.Digest(data), Size: int64(len(data))})
	}
	envelope, err := reporterupdate.Sign(c, "fixture-key", key)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, c.ReleaseID, "catalog.json"), envelope, 0o600))
	return c.ReleaseID
}

func TestClientInstallerReadinessRequiresHTTPSAndActiveUnpausedRelease(t *testing.T) {
	id := prepareControllerSignedRelease(t)
	check := func(origin string, want bool) {
		ctx, w := newAuthenticatedContext(t, http.MethodGet, origin+"/api/external-usage/self/client-installer-options", nil, 7)
		GetExternalUsageSelfClientInstallerOptions(ctx)
		response := decodeAPIResponse(t, w)
		require.True(t, response.Success, response.Message)
		var data struct {
			Available bool `json:"automatic_updates_available"`
		}
		require.NoError(t, common.Unmarshal(response.Data, &data))
		assert.Equal(t, want, data.Available)
	}
	check("https://gateway.example", false)
	require.NoError(t, service.ActivateReporterRelease(id, 7))
	check("http://gateway.example", false)
	check("https://gateway.example", true)
	require.NoError(t, model.PauseReporterDeployment())
	check("https://gateway.example", false)
}

func TestClientInstallerGeneratesSignedAutoOnAndOffWithoutClaudeAddress(t *testing.T) {
	id := prepareControllerSignedRelease(t)
	require.NoError(t, service.ActivateReporterRelease(id, 7))
	for _, automatic := range []bool{true, false} {
		ctx, w := newAuthenticatedContext(t, http.MethodPost, "https://gateway.example/api/external-usage/self/client-installer-command", map[string]any{"platform": "macos", "sources": []string{"codex", "claude_code"}, "auto_update": automatic, "managed_install": true}, 7)
		CreateExternalUsageSelfClientInstallerCommand(ctx)
		response := decodeAPIResponse(t, w)
		require.True(t, response.Success, response.Message)
		var data struct {
			Command string `json:"command"`
		}
		require.NoError(t, common.Unmarshal(response.Data, &data))
		start := strings.Index(data.Command, "https://")
		require.GreaterOrEqual(t, start, 0)
		end := strings.Index(data.Command[start:], "'")
		require.Positive(t, end)
		parsed, err := url.Parse(data.Command[start : start+end])
		require.NoError(t, err)
		assert.Equal(t, map[bool]string{true: "true", false: "false"}[automatic], parsed.Query().Get("auto_update"))
		request, script := newAuthenticatedContext(t, http.MethodGet, parsed.String(), nil, 7)
		request.Params = gin.Params{{Key: "script", Value: "install-clients.sh"}}
		GetExternalUsageClientInstallScript(request)
		assert.Equal(t, "no-store", script.Header().Get("Cache-Control"))
		assert.Contains(t, script.Body.String(), "--install-clients")
		assert.Contains(t, script.Body.String(), "--install-auto-update="+parsed.Query().Get("auto_update"))
		assert.Contains(t, script.Body.String(), "--claude-install-token")
		assert.Contains(t, script.Body.String(), id)
		assert.NotContains(t, script.Body.String(), "/reporter/binary/")
	}
}

func TestUnsignedManagedInstallDoesNotIssueTokens(t *testing.T) {
	prepareControllerSignedRelease(t)
	for _, origin := range []string{"http://gateway.example", "https://gateway.example"} {
		ctx, w := newAuthenticatedContext(t, http.MethodPost, origin+"/api/external-usage/self/client-installer-command", map[string]any{"platform": "macos", "sources": []string{"codex", "claude_code"}, "auto_update": true}, 7)
		CreateExternalUsageSelfClientInstallerCommand(ctx)
		assert.False(t, decodeAPIResponse(t, w).Success)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.ExternalUsageInstallToken{}).Count(&count).Error)
	assert.Zero(t, count)
}
