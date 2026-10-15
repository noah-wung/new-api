package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetReporterNoticeVersion(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) || !ensureExternalUsageSourceEnabled(c, c.DefaultQuery("source", model.ExternalUsageSourceCodex)) {
		return
	}
	version, err := service.ReporterNoticeVersion(c.Query("target"))
	c.Header("Cache-Control", "no-store")
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	common.ApiSuccess(c, version)
}

func GetReporterUpdateCatalog(c *gin.Context) {
	data, _, e := service.ActiveReporterCatalog()
	c.Header("Cache-Control", "no-store")
	if e != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}
func GetReporterReleaseAsset(c *gin.Context) {
	path, e := service.ResolveReporterReleaseAsset(c.Param("release_id"), c.Param("target"))
	if e != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if !strings.HasSuffix(path, "/"+c.Param("name")) && !strings.HasSuffix(path, `\`+c.Param("name")) {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.File(path)
}
func ReportReporterStatus(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	parts := strings.SplitN(strings.TrimSpace(c.GetHeader("Authorization")), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		c.Status(http.StatusUnauthorized)
		return
	}
	device, e := service.LookupExternalUsageDeviceByCredential(parts[1])
	if e != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	var status model.ReporterDeviceStatus
	if e = common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 4096), &status); e != nil {
		common.ApiErrorMsg(c, "invalid reporter status")
		return
	}
	if e = service.SaveReporterDeviceStatus(device, status); e != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "reporter status rejected"})
		return
	}
	common.ApiSuccess(c, nil)
}
func ListSelfReporterStatuses(c *gin.Context) {
	if !ensureExternalUsageEnabled(c) {
		return
	}
	rows, e := model.ListReporterStatuses(c.GetInt("id"))
	if e != nil {
		common.ApiError(c, e)
		return
	}
	common.ApiSuccess(c, rows)
}
func ListReporterReleases(c *gin.Context) {
	rows, e := model.ListReporterReleases()
	if e != nil {
		common.ApiError(c, e)
		return
	}
	d, _ := model.GetReporterDeployment()
	common.ApiSuccess(c, gin.H{"releases": rows, "deployment": d})
}
func ActivateReporterRelease(c *gin.Context) {
	var req struct {
		ReleaseID string `json:"release_id"`
	}
	if common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 2048), &req) != nil {
		common.ApiErrorMsg(c, "invalid release")
		return
	}
	if e := service.ActivateReporterRelease(req.ReleaseID, c.GetInt("id")); e != nil {
		common.ApiErrorMsg(c, "signed release validation failed")
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeSystem, "Activated reporter release "+req.ReleaseID)
	common.ApiSuccess(c, nil)
}
func PauseReporterRelease(c *gin.Context) {
	if e := model.PauseReporterDeployment(); e != nil {
		common.ApiError(c, e)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeSystem, "Paused reporter release distribution")
	common.ApiSuccess(c, nil)
}
