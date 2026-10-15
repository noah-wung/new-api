package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
)

func CreateExternalUsageSelfClaudeInstallerCommand(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) || !ensureExternalUsageSourceEnabled(c, model.ExternalUsageSourceClaudeCode) {
		return
	}
	var req struct {
		Platform          string `json:"platform"`
		UpstreamURL       string `json:"upstream_url"`
		UpstreamIsGateway bool   `json:"upstream_is_gateway"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := service.ValidateInstallerPlatform(req.Platform); err != nil {
		common.ApiError(c, err)
		return
	}
	token, raw, err := service.IssueClaudeInstallToken(c.GetInt("id"), req.UpstreamURL, req.UpstreamIsGateway)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	command, err := service.BuildInstallerCommand(service.BuildInstallScriptParams{
		Source: model.ExternalUsageSourceClaudeCode, Platform: req.Platform,
		ServiceURL: resolveExternalUsageInstallerServiceURL(c), InstallToken: raw,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"command": command, "expires_at": token.ExpiresAt})
}

func ExchangeExternalUsageSelfClaudeInstallerToken(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) || !ensureExternalUsageSourceEnabled(c, model.ExternalUsageSourceClaudeCode) {
		return
	}
	var req struct {
		InstallToken        string `json:"install_token"`
		DeviceName          string `json:"device_name"`
		DeviceFingerprint   string `json:"device_fingerprint"`
		ReportingCredential string `json:"reporting_credential"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	result, err := service.ConsumeClaudeInstallToken(service.ConsumeClaudeInstallTokenParams{
		InstallToken: req.InstallToken, DeviceName: req.DeviceName, DeviceFingerprint: req.DeviceFingerprint, ReportingCredential: req.ReportingCredential,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"server": resolveExternalUsageInstallerServiceURL(c), "source": model.ExternalUsageSourceClaudeCode,
		"device": result.Device, "reporting_credential": result.ReportingCredential,
		"upstream_url": result.UpstreamURL, "upstream_is_gateway": result.UpstreamIsGateway,
		"accept_window_days": external_usage_setting.GetSetting().AcceptWindowDays,
	})
}
