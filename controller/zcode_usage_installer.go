package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
)

func ExchangeExternalUsageSelfZCodeInstallerToken(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) || !ensureExternalUsageSourceEnabled(c, model.ExternalUsageSourceZCode) {
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
	result, err := service.ConsumeZCodeInstallToken(service.ConsumeZCodeInstallTokenParams{InstallToken: req.InstallToken, DeviceName: req.DeviceName, DeviceFingerprint: req.DeviceFingerprint, ReportingCredential: req.ReportingCredential})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"server": resolveExternalUsageInstallerServiceURL(c), "source": model.ExternalUsageSourceZCode, "device": result.Device, "reporting_credential": result.ReportingCredential, "accept_window_days": external_usage_setting.GetSetting().AcceptWindowDays})
}
