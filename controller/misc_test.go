package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
)

func TestGetStatusIncludesExternalUsageConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	raw := config.GlobalConfig.Get("external_usage_setting")
	cfg, ok := raw.(*external_usage_setting.ExternalUsageSetting)
	if !ok || cfg == nil {
		t.Fatal("external_usage_setting config is not registered")
	}
	previous := *cfg
	*cfg = external_usage_setting.ExternalUsageSetting{
		Enabled:             true,
		MaxDevicesPerUser:   99,
		DetailRetentionDays: 30,
		AcceptWindowDays:    7,
		AllowedSources:      []string{"Codex", " zcode ", "", "codex"},
	}
	t.Cleanup(func() {
		*cfg = previous
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	GetStatus(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("response.Success = false, message=%q", response.Message)
	}

	var data map[string]any
	if err := common.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode status payload: %v", err)
	}

	if enabled, ok := data["external_usage_enabled"].(bool); !ok || !enabled {
		t.Fatalf("external_usage_enabled = %#v, want true", data["external_usage_enabled"])
	}
	if maxDevices, ok := data["external_usage_max_devices_per_user"].(float64); !ok || int(maxDevices) != 3 {
		t.Fatalf("external_usage_max_devices_per_user = %#v, want 3", data["external_usage_max_devices_per_user"])
	}
	if acceptWindow, ok := data["external_usage_accept_window_days"].(float64); !ok || int(acceptWindow) != 7 {
		t.Fatalf("external_usage_accept_window_days = %#v, want 7", data["external_usage_accept_window_days"])
	}
	if retentionDays, ok := data["external_usage_detail_retention_days"].(float64); !ok || int(retentionDays) != 30 {
		t.Fatalf("external_usage_detail_retention_days = %#v, want 30", data["external_usage_detail_retention_days"])
	}
	allowedSources, ok := data["external_usage_allowed_sources"].([]any)
	if !ok {
		t.Fatalf("external_usage_allowed_sources = %#v, want []any", data["external_usage_allowed_sources"])
	}
	if len(allowedSources) != 2 || allowedSources[0] != "codex" || allowedSources[1] != "zcode" {
		t.Fatalf("external_usage_allowed_sources = %#v, want [codex zcode]", allowedSources)
	}
}
