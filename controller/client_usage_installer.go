package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
)

func GetExternalUsageSelfClientInstallerOptions(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	common.ApiSuccess(c, gin.H{"automatic_updates_available": service.SignedClientInstallerAvailable(resolveExternalUsageInstallerServiceURL(c))})
}

func CreateExternalUsageSelfClientInstallerCommand(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	var req struct {
		Platform          string   `json:"platform"`
		Sources           []string `json:"sources"`
		UpstreamURL       string   `json:"upstream_url"`
		UpstreamIsGateway bool     `json:"upstream_is_gateway"`
		AutoUpdate        *bool    `json:"auto_update"`
		ManagedInstall    bool     `json:"managed_install"`
		Interactive       bool     `json:"interactive"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := service.ValidateInstallerPlatform(req.Platform); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Sources) == 0 || len(req.Sources) > 2 {
		common.ApiErrorMsg(c, "select Codex, Claude Code, or both")
		return
	}
	selected := make(map[string]bool)
	for _, source := range req.Sources {
		if selected[source] || source != model.ExternalUsageSourceCodex && source != model.ExternalUsageSourceClaudeCode {
			common.ApiErrorMsg(c, "invalid or duplicate client collector")
			return
		}
		selected[source] = true
		if !ensureExternalUsageSourceEnabled(c, source) {
			return
		}
	}
	var tokens service.ClientInstallTokens
	params := service.BuildInstallScriptParams{Interactive: req.Interactive, Platform: req.Platform, ServiceURL: resolveExternalUsageInstallerServiceURL(c)}
	if req.ManagedInstall || req.AutoUpdate != nil && *req.AutoUpdate {
		params.Managed = true
		params.AutoUpdate = req.AutoUpdate != nil && *req.AutoUpdate
		if !service.SignedClientInstallerAvailable(params.ServiceURL) {
			common.ApiErrorMsg(c, "Automatic updates require HTTPS and an active signed release.")
			return
		}
	}
	var expiresAt int64
	if selected[model.ExternalUsageSourceClaudeCode] {
		token, raw, err := service.IssueClaudeInstallToken(c.GetInt("id"), req.UpstreamURL, req.UpstreamIsGateway)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		tokens.Claude, expiresAt = raw, token.ExpiresAt
	}
	if selected[model.ExternalUsageSourceCodex] {
		token, raw, err := service.IssueCodexInstallToken(c.GetInt("id"))
		if err != nil {
			common.ApiError(c, err)
			return
		}
		tokens.Codex = raw
		if expiresAt == 0 || token.ExpiresAt < expiresAt {
			expiresAt = token.ExpiresAt
		}
	}
	command, err := service.BuildClientInstallerCommand(params, tokens)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"command": command, "expires_at": expiresAt})
}

func GetExternalUsageClientInstallScript(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	if value := c.Query("interactive"); value != "" && value != "true" && value != "false" {
		common.ApiErrorMsg(c, "invalid interactive installation option")
		return
	}
	managed := c.Query("auto_update") != ""
	if managed && c.Query("auto_update") != "true" && c.Query("auto_update") != "false" {
		common.ApiErrorMsg(c, "invalid automatic update option")
		return
	}
	tokens := service.ClientInstallTokens{Codex: c.Query("codex_token"), Claude: c.Query("claude_token")}
	if err := service.ValidateClientInstallTokens(tokens); err != nil {
		common.ApiError(c, err)
		return
	}
	platform := service.ExternalUsageInstallerPlatformMacOS
	if strings.TrimSpace(c.Param("script")) == "install-clients.ps1" {
		platform = service.ExternalUsageInstallerPlatformWindows
	}
	script, err := service.BuildClientInstallScript(service.BuildInstallScriptParams{
		Interactive: c.Query("interactive") == "true",
		Managed:     managed, AutoUpdate: c.Query("auto_update") == "true",
		Platform: platform, ServiceURL: resolveExternalUsageInstallerServiceURL(c),
		AcceptWindowDays: external_usage_setting.GetSetting().AcceptWindowDays,
	}, tokens)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
}
