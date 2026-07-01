package controller

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
)

type externalUsageInstallerCommandResponse struct {
	Command   string `json:"command"`
	ExpiresAt int64  `json:"expires_at"`
}

type externalUsageInstallerExchangeResponse struct {
	Source              string                     `json:"source"`
	ReportingCredential string                     `json:"reporting_credential"`
	CredentialPreview   string                     `json:"credential_preview"`
	Device              *model.ExternalUsageDevice `json:"device"`
}

func setupExternalUsageInstallerControllerTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	if err := db.AutoMigrate(
		&model.ExternalUsageDevice{},
		&model.ExternalUsageInstallToken{},
	); err != nil {
		t.Fatalf("failed to migrate installer test tables: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM external_usage_install_tokens")
		db.Exec("DELETE FROM external_usage_devices")
	})
}

func TestCreateExternalUsageSelfCodexInstallerCommand_Success(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "https://gateway.example.com/api/external-usage/self/codex-installer-command", map[string]any{
		"platform": service.ExternalUsageInstallerPlatformMacOS,
	}, 7)

	CreateExternalUsageSelfCodexInstallerCommand(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("response.Success = false, message=%q", response.Message)
	}

	var data externalUsageInstallerCommandResponse
	if err := common.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode command response: %v", err)
	}
	if data.ExpiresAt <= 0 {
		t.Fatalf("data.ExpiresAt = %d, want positive expiry", data.ExpiresAt)
	}
	if !strings.Contains(data.Command, "https://gateway.example.com/api/external-usage/reporter/install.sh?token=") {
		t.Fatalf("data.Command = %q, want install.sh URL for request origin", data.Command)
	}

	var tokens []model.ExternalUsageInstallToken
	if err := model.DB.Find(&tokens).Error; err != nil {
		t.Fatalf("failed to query install tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("len(tokens) = %d, want 1", len(tokens))
	}
	if tokens[0].UserID != 7 {
		t.Fatalf("tokens[0].UserID = %d, want 7", tokens[0].UserID)
	}
}

func TestCreateExternalUsageSelfCodexInstallerCommand_RejectsUnsupportedPlatform(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/external-usage/self/codex-installer-command", map[string]any{
		"platform": "linux",
	}, 7)

	CreateExternalUsageSelfCodexInstallerCommand(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatal("expected unsupported platform request to fail")
	}
	if !strings.Contains(response.Message, "unsupported installer platform") {
		t.Fatalf("response.Message = %q, want unsupported installer platform error", response.Message)
	}

	var tokens []model.ExternalUsageInstallToken
	if err := model.DB.Find(&tokens).Error; err != nil {
		t.Fatalf("failed to query install tokens: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("len(tokens) = %d, want 0 when platform is unsupported", len(tokens))
	}
}

func TestCreateExternalUsageSelfCodexInstallerCommand_RejectsDisabledCodexSource(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)

	raw := config.GlobalConfig.Get("external_usage_setting")
	cfg, ok := raw.(*external_usage_setting.ExternalUsageSetting)
	if !ok || cfg == nil {
		t.Fatal("external_usage_setting config is not registered")
	}
	previous := *cfg
	cfg.AllowedSources = []string{model.ExternalUsageSourceCursor}
	t.Cleanup(func() {
		*cfg = previous
	})

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/external-usage/self/codex-installer-command", map[string]any{
		"platform": service.ExternalUsageInstallerPlatformMacOS,
	}, 7)

	CreateExternalUsageSelfCodexInstallerCommand(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatal("expected disabled codex source to reject installer command generation")
	}
	if !strings.Contains(response.Message, "source is not enabled") {
		t.Fatalf("response.Message = %q, want disabled source error", response.Message)
	}
}

func TestGetExternalUsageReporterInstallScript_RendersExchangeContent(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)

	_, rawToken, err := service.IssueCodexInstallToken(9)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken returned error: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "https://gateway.example.com/api/external-usage/reporter/install.sh?token="+rawToken, nil, 0)
	ctx.Request.Host = "gateway.example.com"
	ctx.Request.URL.Scheme = "https"
	ctx.Params = gin.Params{{Key: "script", Value: "install.sh"}}

	GetExternalUsageReporterInstallScript(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "INSTALL_TOKEN=\""+rawToken+"\"") {
		t.Fatalf("script body missing install token, got %q", body)
	}
	if !strings.Contains(body, "EXCHANGE_URL=\"$SERVICE_URL/api/external-usage/self/codex-installer/exchange\"") {
		t.Fatalf("script body missing exchange endpoint, got %q", body)
	}
}

func TestGetExternalUsageReporterInstallScript_RejectsConsumedToken(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)

	token, rawToken, err := service.IssueCodexInstallToken(9)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken returned error: %v", err)
	}
	token.Status = model.ExternalUsageStatusConsumed
	token.ConsumedAt = common.GetTimestamp()
	if err := model.UpdateExternalUsageInstallToken(nil, token); err != nil {
		t.Fatalf("UpdateExternalUsageInstallToken error: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "https://gateway.example.com/api/external-usage/reporter/install.sh?token="+rawToken, nil, 0)
	ctx.Request.Host = "gateway.example.com"
	ctx.Request.URL.Scheme = "https"
	ctx.Params = gin.Params{{Key: "script", Value: "install.sh"}}

	GetExternalUsageReporterInstallScript(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatal("expected consumed token to reject installer script rendering")
	}
	if !strings.Contains(response.Message, "install token") {
		t.Fatalf("response.Message = %q, want install token error", response.Message)
	}
}

func TestGetExternalUsageReporterBinary_ServesConfiguredAsset(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "darwin-arm64", "usage-reporter")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(path, []byte("binary-data"), 0o755); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	t.Setenv("NEW_API_USAGE_REPORTER_ASSET_DIR", dir)

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/external-usage/reporter/binary/darwin-arm64/usage-reporter", nil, 0)
	ctx.Params = gin.Params{
		{Key: "target", Value: "darwin-arm64"},
		{Key: "name", Value: "usage-reporter"},
	}

	GetExternalUsageReporterBinary(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.String() != "binary-data" {
		t.Fatalf("recorder.Body = %q, want %q", recorder.Body.String(), "binary-data")
	}
}

func TestCreateExternalUsageSelfCodexInstallerCommand_SelfOnlyForAdmin(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "https://gateway.example.com/api/external-usage/self/codex-installer-command?user_id=99", map[string]any{
		"platform": service.ExternalUsageInstallerPlatformWindows,
	}, 7)
	ctx.Set("role", common.RoleRootUser)

	CreateExternalUsageSelfCodexInstallerCommand(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("response.Success = false, message=%q", response.Message)
	}

	var tokens []model.ExternalUsageInstallToken
	if err := model.DB.Order("id asc").Find(&tokens).Error; err != nil {
		t.Fatalf("failed to query install tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("len(tokens) = %d, want 1", len(tokens))
	}
	if tokens[0].UserID != 7 {
		t.Fatalf("tokens[0].UserID = %d, want 7; self endpoint must ignore inspected user", tokens[0].UserID)
	}
}

func TestExchangeExternalUsageSelfCodexInstallerToken_Success(t *testing.T) {
	setupExternalUsageInstallerControllerTestDB(t)
	enableExternalUsageForTest(t)

	_, rawToken, err := service.IssueCodexInstallToken(12)
	if err != nil {
		t.Fatalf("IssueCodexInstallToken returned error: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/external-usage/self/codex-installer/exchange", map[string]any{
		"install_token":      rawToken,
		"device_name":        "Codex Workstation",
		"device_fingerprint": "codex-device-fingerprint-1",
	}, 0)

	ExchangeExternalUsageSelfCodexInstallerToken(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("response.Success = false, message=%q", response.Message)
	}

	var data externalUsageInstallerExchangeResponse
	if err := common.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode exchange response: %v", err)
	}
	if data.Source != model.ExternalUsageSourceCodex {
		t.Fatalf("data.Source = %q, want %q", data.Source, model.ExternalUsageSourceCodex)
	}
	if strings.TrimSpace(data.ReportingCredential) == "" {
		t.Fatal("expected non-empty reporting credential")
	}
	if data.Device == nil {
		t.Fatal("expected exchange response to include device")
	}
	if data.Device.UserID != 12 {
		t.Fatalf("data.Device.UserID = %d, want 12", data.Device.UserID)
	}
}
