package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeReporterBinaryDownloadWithCodexDisabled(t *testing.T) {
	cfg, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled = true
	cfg.AllowedSources = []string{model.ExternalUsageSourceClaudeCode}
	dir := t.TempDir()
	path := filepath.Join(dir, "darwin-arm64", "usage-reporter")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("fixture-binary"), 0o700))
	t.Setenv("NEW_API_USAGE_REPORTER_ASSET_DIR", dir)
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/external-usage/reporter/binary/darwin-arm64/usage-reporter?source=claude_code", nil, 0)
	ctx.Params = gin.Params{{Key: "target", Value: "darwin-arm64"}, {Key: "name", Value: "usage-reporter"}}
	GetExternalUsageReporterBinary(ctx)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "fixture-binary", recorder.Body.String())
}

func TestClaudeTelemetryToReportAPIExactlyOnceWithoutBilling(t *testing.T) {
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.ExternalUsageDevice{}, &model.ExternalUsageClientEvent{}, &model.ExternalUsageReportBatch{}, &model.ExternalUsageAggregate{}, &model.ExternalUsageModelMapping{}))
	cfg, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled = true
	cfg.AcceptWindowDays = 90
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex, model.ExternalUsageSourceClaudeCode}
	user := model.User{Username: "claude-usage", Password: "fixture-only", Quota: 50000}
	require.NoError(t, db.Create(&user).Error)
	device, credential, err := service.IssueExternalUsageDeviceCredential(service.IssueExternalUsageDeviceParams{UserID: user.Id, DeviceFingerprint: "claude-usage-test", DeviceName: "test", AllowedSources: []string{model.ExternalUsageSourceClaudeCode}})
	require.NoError(t, err)
	api := gin.New()
	api.POST("/api/external-usage/report", ReportExternalUsage)
	var loseACK atomic.Bool
	loseACK.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, r)
		if loseACK.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	reporterCfg := usagereporter.Config{Server: server.URL, Credential: credential, Source: usagereporter.SourceClaudeCode, StatePath: filepath.Join(t.TempDir(), "state.json"), AcceptWindowDays: 90, ClaudeReceiverToken: strings.Repeat("a", 64)}
	receiver, err := usagereporter.NewClaudeReceiver(reporterCfg)
	require.NoError(t, err)
	when := time.Now().Add(-time.Minute).UnixNano()
	logs := make([]any, 0, 3)
	for i, entrypoint := range []string{"cli", "claude-vscode", "cli"} {
		attrs := []any{}
		for key, value := range map[string]string{"event.name": "api_request", "request_id": fmt.Sprintf("request-%d", i), "session.id": "same-session", "app.entrypoint": entrypoint, "model": "claude-test", "input_tokens": "100", "cache_read_tokens": "200", "cache_creation_tokens": "300", "output_tokens": "40"} {
			attrs = append(attrs, map[string]any{"key": key, "value": map[string]string{"stringValue": value}})
		}
		logs = append(logs, map[string]any{"timeUnixNano": fmt.Sprint(when), "attributes": attrs})
	}
	telemetry, err := common.Marshal(map[string]any{"resourceLogs": []any{map[string]any{"resource": map[string]any{"attributes": []any{map[string]any{"key": usagereporter.ClaudeUpstreamAttribute, "value": map[string]string{"stringValue": "https://api.anthropic.com"}}}}, "scopeLogs": []any{map[string]any{"logRecords": logs}}}}})
	require.NoError(t, err)
	export := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(telemetry))
	export.Header.Set("Content-Type", "application/json")
	export.Header.Set("Authorization", "Bearer "+reporterCfg.ClaudeReceiverToken)
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, export)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "{}", response.Body.String())
	require.Error(t, receiver.Flush(), "server committed, but the client did not receive the ACK")
	var events []model.ExternalUsageClientEvent
	require.NoError(t, db.Order("id").Find(&events).Error)
	require.Len(t, events, 3, "identical token counts in one session second still represent separate requests")
	for _, event := range events {
		assert.Equal(t, model.ExternalUsageSourceClaudeCode, event.Source)
		assert.Equal(t, "claude_code:claude-test", event.NormalizedModelName)
		assert.Equal(t, 600, event.InputToken)
		assert.Equal(t, 200, event.CachedInputToken)
		assert.Equal(t, 640, event.TotalToken)
	}
	loseACK.Store(false)
	restarted, err := usagereporter.NewClaudeReceiver(reporterCfg)
	require.NoError(t, err)
	require.NoError(t, restarted.Flush())
	var aggregates []model.ExternalUsageAggregate
	require.NoError(t, db.Find(&aggregates).Error)
	require.Len(t, aggregates, 1)
	assert.Equal(t, int64(3), aggregates[0].EventCount)
	assert.Equal(t, int64(1920), aggregates[0].TotalTokens)
	var reloadedUser model.User
	require.NoError(t, db.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, 50000, reloadedUser.Quota)
	assert.Zero(t, reloadedUser.UsedQuota)
	assert.Zero(t, reloadedUser.RequestCount)
	var gatewayLogs int64
	require.NoError(t, db.Model(&model.Log{}).Count(&gatewayLogs).Error)
	assert.Zero(t, gatewayLogs)
	// Site and per-device authorizations remain independent.
	params := service.ReportExternalUsageParams{Source: model.ExternalUsageSourceClaudeCode}
	require.NoError(t, device.SetAllowedSources([]string{model.ExternalUsageSourceCodex}))
	_, err = service.ReportExternalUsage(device, params)
	assert.ErrorContains(t, err, "not allowed for this credential")
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex}
	_, err = service.ReportExternalUsage(device, params)
	assert.ErrorContains(t, err, "not enabled")
}
