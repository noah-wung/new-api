package controller

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

type createExternalUsageDeviceRequest struct {
	DeviceName        string   `json:"device_name"`
	DeviceFingerprint string   `json:"device_fingerprint"`
	AllowedSources    []string `json:"allowed_sources"`
}

type createExternalUsageInstallerCommandRequest struct {
	Platform string `json:"platform"`
}

type exchangeExternalUsageInstallerTokenRequest struct {
	InstallToken      string `json:"install_token"`
	DeviceName        string `json:"device_name"`
	DeviceFingerprint string `json:"device_fingerprint"`
}

type deleteExternalUsageModelDataRequest struct {
	UserID              int    `json:"user_id"`
	Source              string `json:"source"`
	NormalizedModelName string `json:"normalized_model_name"`
}

type upsertExternalUsageModelMappingRequest struct {
	Source              string `json:"source"`
	SourceModelName     string `json:"source_model_name"`
	NormalizedModelName string `json:"normalized_model_name"`
}

type rebuildExternalUsageAggregatesRequest struct {
	UserID    int    `json:"user_id"`
	Source    string `json:"source"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
}

func resolveExternalUsageTargetUserID(c *gin.Context, fallbackUserID int) int {
	targetUserID := fallbackUserID
	if c.GetInt("role") >= common.RoleAdminUser {
		if parsedUserID, err := strconv.Atoi(strings.TrimSpace(c.Query("user_id"))); err == nil && parsedUserID > 0 {
			targetUserID = parsedUserID
		}
	}
	return targetUserID
}

func ensureExternalUsageEnabled(c *gin.Context) bool {
	if external_usage_setting.IsEnabled() {
		return true
	}
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "external usage is disabled",
	})
	return false
}

func ensureExternalUsageSourceEnabled(c *gin.Context, source string) bool {
	if err := service.ValidateExternalUsageSourceEnabled(source); err == nil {
		return true
	} else {
		common.ApiError(c, err)
		return false
	}
}

func resolveExternalUsageInstallerServiceURL(c *gin.Context) string {
	scheme := strings.TrimSpace(c.Request.URL.Scheme)
	if scheme == "" {
		scheme = strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	}
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := strings.TrimSpace(c.Request.Host)
	if host != "" {
		return strings.TrimRight(scheme+"://"+host, "/")
	}

	return strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
}

func GetExternalUsageOverview(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	userID := resolveExternalUsageTargetUserID(c, c.GetInt("id"))
	startTime, _ := strconv.ParseInt(c.Query("start_time"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_time"), 10, 64)
	data, err := service.BuildExternalUsageOverview(userID, startTime, endTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}

func CreateExternalUsageDeviceCredential(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	var req createExternalUsageDeviceRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	device, rawCredential, err := service.IssueExternalUsageDeviceCredential(service.IssueExternalUsageDeviceParams{
		UserID:            c.GetInt("id"),
		DeviceName:        req.DeviceName,
		DeviceFingerprint: req.DeviceFingerprint,
		AllowedSources:    req.AllowedSources,
	})
	if err != nil {
		if err == model.ErrExternalUsageDeviceLimitExceeded {
			common.ApiErrorMsg(c, service.FormatDuplicateDeviceLimit(external_usage_setting.GetSetting().MaxDevicesPerUser).Error())
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"device":               device,
		"reporting_credential": rawCredential,
		"credential_preview":   service.BuildExternalUsageCredentialDisplay(rawCredential),
	})
}

func CreateExternalUsageSelfCodexInstallerCommand(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	if !ensureExternalUsageSourceEnabled(c, model.ExternalUsageSourceCodex) {
		return
	}
	var req createExternalUsageInstallerCommandRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if err := service.ValidateInstallerPlatform(req.Platform); err != nil {
		common.ApiError(c, err)
		return
	}

	installToken, rawInstallToken, err := service.IssueCodexInstallToken(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	command, err := service.BuildInstallerCommand(service.BuildInstallScriptParams{
		Platform:     req.Platform,
		ServiceURL:   resolveExternalUsageInstallerServiceURL(c),
		InstallToken: rawInstallToken,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"command":    command,
		"expires_at": installToken.ExpiresAt,
	})
}

func ExchangeExternalUsageSelfCodexInstallerToken(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	if !ensureExternalUsageSourceEnabled(c, model.ExternalUsageSourceCodex) {
		return
	}
	var req exchangeExternalUsageInstallerTokenRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}

	result, err := service.ConsumeCodexInstallToken(service.ConsumeCodexInstallTokenParams{
		InstallToken:      req.InstallToken,
		DeviceName:        req.DeviceName,
		DeviceFingerprint: req.DeviceFingerprint,
	})
	if err != nil {
		if err == model.ErrExternalUsageDeviceLimitExceeded {
			common.ApiErrorMsg(c, service.FormatDuplicateDeviceLimit(external_usage_setting.GetSetting().MaxDevicesPerUser).Error())
			return
		}
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"server":               resolveExternalUsageInstallerServiceURL(c),
		"source":               result.Source,
		"device":               result.Device,
		"reporting_credential": result.ReportingCredential,
		"credential_preview":   service.BuildExternalUsageCredentialDisplay(result.ReportingCredential),
	})
}

func GetExternalUsageReporterInstallScript(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	if !ensureExternalUsageSourceEnabled(c, model.ExternalUsageSourceCodex) {
		return
	}

	platform := ""
	switch strings.TrimSpace(c.Param("script")) {
	case "install.sh":
		platform = service.ExternalUsageInstallerPlatformMacOS
	case "install.ps1":
		platform = service.ExternalUsageInstallerPlatformWindows
	default:
		common.ApiErrorMsg(c, "unsupported installer script")
		return
	}

	if _, err := service.ValidateCodexInstallToken(c.Query("token")); err != nil {
		common.ApiError(c, err)
		return
	}

	script, err := service.BuildInstallScript(service.BuildInstallScriptParams{
		Platform:         platform,
		ServiceURL:       resolveExternalUsageInstallerServiceURL(c),
		InstallToken:     c.Query("token"),
		AcceptWindowDays: external_usage_setting.GetSetting().AcceptWindowDays,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(script))
}

func GetExternalUsageReporterBinary(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	if !ensureExternalUsageSourceEnabled(c, model.ExternalUsageSourceCodex) {
		return
	}

	spec, err := service.ResolveUsageReporterBinarySpec(c.Param("target"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if strings.TrimSpace(c.Param("name")) != spec.DownloadName {
		common.ApiErrorMsg(c, "unsupported reporter binary name")
		return
	}

	c.FileAttachment(spec.Path, spec.DownloadName)
}

func ListExternalUsageDevices(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	userID := resolveExternalUsageTargetUserID(c, c.GetInt("id"))
	devices, err := model.ListExternalUsageDevicesByUser(userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, devices)
}

func RevokeExternalUsageDevice(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	deviceID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "invalid device id")
		return
	}
	if err := service.RevokeExternalUsageDevice(c.GetInt("id"), deviceID); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": deviceID})
}

func ReportExternalUsage(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "missing authorization header",
		})
		return
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid authorization header",
		})
		return
	}
	device, err := service.LookupExternalUsageDeviceByCredential(parts[1])
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	var req service.ReportExternalUsageParams
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	result, err := service.ReportExternalUsage(device, req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func ImportExternalUsageCursorCSV(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	targetUserID, err := strconv.Atoi(c.PostForm("user_id"))
	if err != nil || targetUserID <= 0 {
		common.ApiErrorMsg(c, "invalid target user id")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorMsg(c, "missing csv file")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.ImportCursorUsageCSV(c.GetInt("id"), targetUserID, fileHeader.Filename, content)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func ListExternalUsageCursorImportBatches(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	pageInfo := common.GetPageQuery(c)
	targetUserID, _ := strconv.Atoi(c.Query("user_id"))
	if targetUserID <= 0 {
		targetUserID = c.GetInt("id")
	}
	isAdmin := c.GetInt("role") >= common.RoleAdminUser
	batches, total, err := model.ListExternalUsageCursorImportBatches(targetUserID, isAdmin, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(batches)
	common.ApiSuccess(c, pageInfo)
}

func ListExternalUsageReportBatches(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	pageInfo := common.GetPageQuery(c)
	targetUserID := resolveExternalUsageTargetUserID(c, c.GetInt("id"))
	rows, total, err := service.ListExternalUsageReportBatches(targetUserID, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

func ListExternalUsageDetails(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	pageInfo := common.GetPageQuery(c)
	rows, total, err := service.ListExternalUsageDetails(service.ListExternalUsageDetailsParams{
		UserID: resolveExternalUsageTargetUserID(c, c.GetInt("id")),
		Source: c.Query("source"),
		Offset: pageInfo.GetStartIdx(),
		Limit:  pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

func ListExternalUsageModelMappings(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	rows, err := service.ListExternalUsageModelMappings(c.Query("source"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func UpsertExternalUsageModelMapping(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	var req upsertExternalUsageModelMappingRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	result, err := service.UpsertExternalUsageModelMapping(service.UpsertExternalUsageModelMappingParams{
		Source:              req.Source,
		SourceModelName:     req.SourceModelName,
		NormalizedModelName: req.NormalizedModelName,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func DeleteExternalUsageModelMapping(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid mapping id")
		return
	}
	result, err := service.DeleteExternalUsageModelMapping(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func DeleteExternalUsageCursorImportBatch(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	batchID, err := strconv.Atoi(c.Param("id"))
	if err != nil || batchID <= 0 {
		common.ApiErrorMsg(c, "invalid batch id")
		return
	}
	result, err := service.DeleteCursorImportBatch(batchID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func ReplayExternalUsageCursorImportBatch(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	batchID, err := strconv.Atoi(c.Param("id"))
	if err != nil || batchID <= 0 {
		common.ApiErrorMsg(c, "invalid batch id")
		return
	}
	result, err := service.ReplayCursorImportBatch(batchID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func RebuildExternalUsageAggregates(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	var req rebuildExternalUsageAggregatesRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if req.UserID <= 0 {
		common.ApiErrorMsg(c, "invalid target user id")
		return
	}
	result, err := service.RebuildExternalUsageAggregates(service.RebuildExternalUsageAggregatesParams{
		UserID:    req.UserID,
		Source:    req.Source,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func DeleteExternalUsageModelData(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	var req deleteExternalUsageModelDataRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if req.UserID <= 0 {
		common.ApiErrorMsg(c, "invalid target user id")
		return
	}
	result, err := service.DeleteExternalUsageModelData(service.DeleteExternalUsageModelDataParams{
		UserID:              req.UserID,
		Source:              req.Source,
		NormalizedModelName: req.NormalizedModelName,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
