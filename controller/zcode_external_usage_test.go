package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReporterReasoningHTTPToAggregate(t *testing.T) {
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ExternalUsageDevice{}, &model.ExternalUsageReportBatch{}, &model.ExternalUsageClientEvent{}, &model.ZCodeUsageLock{}, &model.ZCodeUsageIdentity{}, &model.ExternalUsageAggregate{}, &model.ExternalUsageModelMapping{}))
	cfg := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled = true
	cfg.AcceptWindowDays = 90
	cfg.AllowedSources = []string{model.ExternalUsageSourceZCode}
	device, credential, err := service.IssueExternalUsageDeviceCredential(service.IssueExternalUsageDeviceParams{UserID: 7001, DeviceName: "fixture", DeviceFingerprint: "http-zcode", AllowedSources: []string{model.ExternalUsageSourceZCode}})
	require.NoError(t, err)
	payload := usagereporter.ReportPayload{Source: model.ExternalUsageSourceZCode, Events: []usagereporter.UsageEvent{{EventID: "fixture-id", SessionID: "fixture-session", OccurredAt: time.Now().Unix(), SourceModelName: "fixture-model", InputTokens: 100, OutputTokens: 20, ReasoningOutputTokens: 7, TotalTokens: 120}}}
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	router := gin.New()
	router.POST("/api/external-usage/report", ReportExternalUsage)
	req := httptest.NewRequest(http.MethodPost, "/api/external-usage/report", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+credential)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code)
	var stored model.ExternalUsageClientEvent
	require.NoError(t, db.Where("user_id = ?", device.UserID).First(&stored).Error)
	require.Equal(t, 7, stored.ReasoningOutputToken)
	summary, err := model.ListExternalUsageSummaryByUser(device.UserID, 0, 0)
	require.NoError(t, err)
	require.Len(t, summary, 1)
	require.EqualValues(t, 7, summary[0].ReasoningOutputToken)
	conflictBody, err := common.Marshal(map[string]any{"source": "zcode", "events": []map[string]any{{"event_id": "different", "session_id": "fixture-session", "occurred_at": time.Now().Unix(), "source_model_name": "fixture-model", "input_tokens": 100, "output_tokens": 20, "total_tokens": 120, "reasoning_output_token": 0, "reasoning_output_tokens": 7}}})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/external-usage/report", bytes.NewReader(conflictBody))
	request.Header.Set("Authorization", "Bearer "+credential)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var decoded struct {
		Data service.ReportExternalUsageResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
	require.Len(t, decoded.Data.RejectedEventInfos, 1)
	var count int64
	require.NoError(t, db.Model(&model.ExternalUsageClientEvent{}).Where("user_id = ?", device.UserID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
