package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxAdminUserModelUsageRangeSeconds int64 = 90 * 24 * 3600

var userModelUsagePageSizes = map[int]struct{}{
	20:  {},
	50:  {},
	100: {},
}

var userModelUsageSortFields = map[string]struct{}{
	service.UserModelUsageSortModelName:       {},
	service.UserModelUsageSortTokenUsage:      {},
	service.UserModelUsageSortQuota:           {},
	service.UserModelUsageSortGatewayRequests: {},
	service.UserModelUsageSortExternalEvents:  {},
}

type userModelUsageCollectionStatus struct {
	DataExportEnabled      bool `json:"data_export_enabled"`
	ConsumeLogEnabled      bool `json:"consume_log_enabled"`
	ExternalUsageEnabled   bool `json:"external_usage_enabled"`
	RefreshIntervalMinutes int  `json:"refresh_interval_minutes"`
}

type userModelUsageResponse struct {
	*service.UserModelUsagePage
	CollectionStatus userModelUsageCollectionStatus `json:"collection_status"`
}

func RequireAdminUserModelUsage() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetInt("role") < common.RoleAdminUser {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "forbidden",
			})
			return
		}
		c.Next()
	}
}

func GetUserModelUsage(c *gin.Context) {
	query, err := parseUserModelUsageQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	page, err := service.GetUserModelUsage(query)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "user not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "database error",
		})
		return
	}

	common.ApiSuccess(c, userModelUsageResponse{
		UserModelUsagePage: page,
		CollectionStatus: userModelUsageCollectionStatus{
			DataExportEnabled:      common.DataExportEnabled,
			ConsumeLogEnabled:      common.LogConsumeEnabled,
			ExternalUsageEnabled:   external_usage_setting.IsEnabled(),
			RefreshIntervalMinutes: common.DataExportInterval,
		},
	})
}

func parseUserModelUsageQuery(c *gin.Context) (service.UserModelUsageQuery, error) {
	userID, err := parsePositiveUserModelUsageInt(c.Param("user_id"), "user_id")
	if err != nil {
		return service.UserModelUsageQuery{}, err
	}
	startTime, err := parsePositiveUserModelUsageInt64(c.Query("start_timestamp"), "start_timestamp")
	if err != nil {
		return service.UserModelUsageQuery{}, err
	}
	endTime, err := parsePositiveUserModelUsageInt64(c.Query("end_timestamp"), "end_timestamp")
	if err != nil {
		return service.UserModelUsageQuery{}, err
	}
	if endTime < startTime {
		return service.UserModelUsageQuery{}, errors.New("end_timestamp must not precede start_timestamp")
	}
	if endTime-startTime > maxAdminUserModelUsageRangeSeconds {
		return service.UserModelUsageQuery{}, errors.New("time range must not exceed 90 days")
	}

	page, err := parseOptionalPositiveUserModelUsageInt(c.Query("p"), 1, "p")
	if err != nil {
		return service.UserModelUsageQuery{}, err
	}
	pageSize, err := parseOptionalPositiveUserModelUsageInt(c.Query("page_size"), 20, "page_size")
	if err != nil {
		return service.UserModelUsageQuery{}, err
	}
	if _, ok := userModelUsagePageSizes[pageSize]; !ok {
		return service.UserModelUsageQuery{}, errors.New("page_size must be one of 20, 50, or 100")
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return service.UserModelUsageQuery{}, errors.New("page offset exceeds supported range")
	}

	sortBy := c.Query("sort_by")
	if sortBy == "" {
		sortBy = service.UserModelUsageSortQuota
	}
	if _, ok := userModelUsageSortFields[sortBy]; !ok {
		return service.UserModelUsageQuery{}, errors.New("invalid sort_by")
	}

	sortOrder := c.Query("sort_order")
	if sortOrder == "" {
		sortOrder = service.UserModelUsageSortDesc
		if sortBy == service.UserModelUsageSortModelName {
			sortOrder = service.UserModelUsageSortAsc
		}
	}
	if sortOrder != service.UserModelUsageSortAsc && sortOrder != service.UserModelUsageSortDesc {
		return service.UserModelUsageQuery{}, errors.New("invalid sort_order")
	}

	return service.UserModelUsageQuery{
		UserID:      userID,
		StartTime:   startTime,
		EndTime:     endTime,
		Sources:     parseUserModelUsageSources(c.QueryArray("sources")),
		ModelSearch: strings.TrimSpace(c.Query("model_search")),
		SortBy:      sortBy,
		SortOrder:   sortOrder,
		Page:        page,
		PageSize:    pageSize,
	}, nil
}

func parsePositiveUserModelUsageInt(raw string, field string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, errors.New(field + " must be a positive integer")
	}
	return value, nil
}

func parsePositiveUserModelUsageInt64(raw string, field string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New(field + " must be a positive integer")
	}
	return value, nil
}

func parseOptionalPositiveUserModelUsageInt(raw string, defaultValue int, field string) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	return parsePositiveUserModelUsageInt(raw, field)
}

func parseUserModelUsageSources(rawSources []string) []string {
	if len(rawSources) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	sources := make([]string, 0, len(rawSources))
	for _, rawSource := range rawSources {
		for _, item := range strings.Split(rawSource, ",") {
			source := strings.ToLower(strings.TrimSpace(item))
			if source == "" {
				continue
			}
			if _, ok := seen[source]; ok {
				continue
			}
			seen[source] = struct{}{}
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return nil
	}
	return sources
}
