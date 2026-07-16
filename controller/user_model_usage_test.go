package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userModelUsageAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		User             service.UserModelUsageTarget `json:"user"`
		AvailableSources []string                     `json:"available_sources"`
		Totals           service.UserModelUsageTotals `json:"totals"`
		Items            []service.UserModelUsageItem `json:"items"`
		Page             int                          `json:"page"`
		PageSize         int                          `json:"page_size"`
		Total            int                          `json:"total"`
		CollectionStatus struct {
			DataExportEnabled      bool `json:"data_export_enabled"`
			ConsumeLogEnabled      bool `json:"consume_log_enabled"`
			ExternalUsageEnabled   bool `json:"external_usage_enabled"`
			RefreshIntervalMinutes int  `json:"refresh_interval_minutes"`
		} `json:"collection_status"`
	} `json:"data"`
}

func setupUserModelUsageControllerTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.QuotaData{},
		&model.ExternalUsageAggregate{},
		&model.ExternalUsageModelMapping{},
	))

	target := &model.User{
		Id:          2,
		Username:    "usage-target",
		Password:    "not-a-real-password",
		DisplayName: "Usage Target",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(target).Error)
	require.NoError(t, db.Delete(target).Error)

	require.NoError(t, db.Create(&model.QuotaData{
		UserID:    target.Id,
		Username:  target.Username,
		ModelName: "GPT-5",
		CreatedAt: 1717200000,
		TokenUsed: 100,
		Count:     2,
		Quota:     200,
	}).Error)
	require.NoError(t, db.Create(&model.ExternalUsageAggregate{
		UserID:              target.Id,
		Origin:              model.ExternalUsageOriginClientReport,
		Source:              model.ExternalUsageSourceCodex,
		NormalizedModelName: "gpt-5",
		BucketAt:            1717200000,
		EventCount:          3,
		TotalTokens:         300,
	}).Error)
	require.NoError(t, db.Create(&model.ExternalUsageAggregate{
		UserID:              target.Id,
		Origin:              model.ExternalUsageOriginCursorImport,
		Source:              model.ExternalUsageSourceCursor,
		NormalizedModelName: "cursor:auto",
		BucketAt:            1717200000,
		EventCount:          1,
		TotalTokens:         50,
	}).Error)
}

func setUserModelUsageCollectionGlobals(t *testing.T, dataExport bool, consumeLog bool, externalUsage bool, interval int) {
	t.Helper()

	previousDataExport := common.DataExportEnabled
	previousConsumeLog := common.LogConsumeEnabled
	previousInterval := common.DataExportInterval
	raw := config.GlobalConfig.Get("external_usage_setting")
	setting, ok := raw.(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	require.NotNil(t, setting)
	previousExternalUsage := *setting

	common.DataExportEnabled = dataExport
	common.LogConsumeEnabled = consumeLog
	common.DataExportInterval = interval
	setting.Enabled = externalUsage
	t.Cleanup(func() {
		common.DataExportEnabled = previousDataExport
		common.LogConsumeEnabled = previousConsumeLog
		common.DataExportInterval = previousInterval
		*setting = previousExternalUsage
	})
}

func performUserModelUsageRequest(t *testing.T, role int, target string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("user-model-usage-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", "requester")
		session.Set("role", role)
		session.Set("id", 1)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/data/users/:user_id/models", middleware.UserAuth(), RequireAdminUserModelUsage(), GetUserModelUsage)

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	require.Equal(t, http.StatusNoContent, loginRecorder.Code)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("New-Api-User", "1")
	for _, item := range loginRecorder.Result().Cookies() {
		request.AddCookie(item)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeUserModelUsageResponse(t *testing.T, recorder *httptest.ResponseRecorder) userModelUsageAPIResponse {
	t.Helper()

	var response userModelUsageAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestGetUserModelUsageAdminReturnsContract(t *testing.T) {
	setupUserModelUsageControllerTestDB(t)
	setUserModelUsageCollectionGlobals(t, true, true, true, 7)

	recorder := performUserModelUsageRequest(t, common.RoleRootUser,
		"/api/data/users/2/models?start_timestamp=1717190000&end_timestamp=1717210000&sources=%20GATEWAY,codex&sources=codex&model_search=%20GPT-5%20&sort_by=token_usage&sort_order=desc&p=1&page_size=20")
	require.Equal(t, http.StatusOK, recorder.Code)

	response := decodeUserModelUsageResponse(t, recorder)
	require.True(t, response.Success)
	require.Equal(t, service.UserModelUsageTarget{
		ID: 2, Username: "usage-target", DisplayName: "Usage Target", Status: common.UserStatusEnabled, Deleted: true,
	}, response.Data.User)
	require.Equal(t, []string{
		service.UserModelUsageSourceGateway,
		service.UserModelUsageSourceCodex,
		service.UserModelUsageSourceCursor,
	}, response.Data.AvailableSources)
	require.Equal(t, service.UserModelUsageTotals{
		TokenUsage: 400, Quota: 200, GatewayRequests: 2, ExternalEvents: 3,
	}, response.Data.Totals)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, "gpt-5", response.Data.Items[0].ModelName)
	require.Equal(t, []string{service.UserModelUsageSourceGateway, service.UserModelUsageSourceCodex}, []string{
		response.Data.Items[0].Sources[0].Source,
		response.Data.Items[0].Sources[1].Source,
	})
	require.Equal(t, 1, response.Data.Page)
	require.Equal(t, 20, response.Data.PageSize)
	require.Equal(t, 1, response.Data.Total)
	require.True(t, response.Data.CollectionStatus.DataExportEnabled)
	require.True(t, response.Data.CollectionStatus.ConsumeLogEnabled)
	require.True(t, response.Data.CollectionStatus.ExternalUsageEnabled)
	require.Equal(t, 7, response.Data.CollectionStatus.RefreshIntervalMinutes)
}

func TestGetUserModelUsageCommonUserReceivesForbidden(t *testing.T) {
	setupUserModelUsageControllerTestDB(t)

	recorder := performUserModelUsageRequest(t, common.RoleCommonUser,
		"/api/data/users/2/models?start_timestamp=1717190000&end_timestamp=1717210000")
	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestGetUserModelUsageRejectsInvalidRanges(t *testing.T) {
	tests := map[string]string{
		"missing range":      "/api/data/users/2/models",
		"missing start":      "/api/data/users/2/models?end_timestamp=1717210000",
		"non positive start": "/api/data/users/2/models?start_timestamp=0&end_timestamp=1717210000",
		"reversed range":     "/api/data/users/2/models?start_timestamp=1717210000&end_timestamp=1717190000",
		"over ninety days":   "/api/data/users/2/models?start_timestamp=1&end_timestamp=7776002",
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			setupUserModelUsageControllerTestDB(t)
			recorder := performUserModelUsageRequest(t, common.RoleRootUser, target)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestGetUserModelUsageRejectsInvalidUserID(t *testing.T) {
	for _, userID := range []string{"0", "-1", "not-an-integer"} {
		t.Run(userID, func(t *testing.T) {
			setupUserModelUsageControllerTestDB(t)
			recorder := performUserModelUsageRequest(t, common.RoleRootUser,
				"/api/data/users/"+userID+"/models?start_timestamp=1717190000&end_timestamp=1717210000")
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestGetUserModelUsageRejectsInvalidPaginationAndSorting(t *testing.T) {
	tests := map[string]string{
		"unsupported page size": "page_size=21",
		"zero page":             "p=0",
		"unknown sort":          "sort_by=requests",
		"unknown order":         "sort_order=sideways",
	}
	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			setupUserModelUsageControllerTestDB(t)
			target := "/api/data/users/2/models?start_timestamp=1717190000&end_timestamp=1717210000&" + query
			recorder := performUserModelUsageRequest(t, common.RoleRootUser, target)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestGetUserModelUsageRejectsPageOffsetOverflow(t *testing.T) {
	setupUserModelUsageControllerTestDB(t)
	maxInt := int(^uint(0) >> 1)
	target := "/api/data/users/2/models?start_timestamp=1717190000&end_timestamp=1717210000&p=" + strconv.Itoa(maxInt) + "&page_size=100"

	recorder := performUserModelUsageRequest(t, common.RoleRootUser, target)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestGetUserModelUsageAppliesSortAndPaginationDefaults(t *testing.T) {
	setupUserModelUsageControllerTestDB(t)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID: 2, Username: "usage-target", ModelName: "zeta", CreatedAt: 1717200000, TokenUsed: 1, Count: 1, Quota: 500,
	}).Error)

	recorder := performUserModelUsageRequest(t, common.RoleAdminUser,
		"/api/data/users/2/models?start_timestamp=1717190000&end_timestamp=1717210000&sources=gateway")
	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeUserModelUsageResponse(t, recorder)
	require.Equal(t, []string{"zeta", "gpt-5"}, []string{response.Data.Items[0].ModelName, response.Data.Items[1].ModelName})
	require.Equal(t, 1, response.Data.Page)
	require.Equal(t, 20, response.Data.PageSize)

	recorder = performUserModelUsageRequest(t, common.RoleAdminUser,
		"/api/data/users/2/models?start_timestamp=1717190000&end_timestamp=1717210000&sources=gateway&sort_by=model_name")
	require.Equal(t, http.StatusOK, recorder.Code)
	response = decodeUserModelUsageResponse(t, recorder)
	require.Equal(t, []string{"gpt-5", "zeta"}, []string{response.Data.Items[0].ModelName, response.Data.Items[1].ModelName})
}

func TestGetUserModelUsageUnknownUserReturnsNotFound(t *testing.T) {
	setupUserModelUsageControllerTestDB(t)

	recorder := performUserModelUsageRequest(t, common.RoleRootUser,
		"/api/data/users/999/models?start_timestamp=1717190000&end_timestamp=1717210000")
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestGetUserModelUsageDisabledCollectionGlobalsDoNotHideHistory(t *testing.T) {
	setupUserModelUsageControllerTestDB(t)
	setUserModelUsageCollectionGlobals(t, false, false, false, 13)

	recorder := performUserModelUsageRequest(t, common.RoleRootUser,
		"/api/data/users/2/models?start_timestamp=1717190000&end_timestamp=1717210000&sources=codex")
	require.Equal(t, http.StatusOK, recorder.Code)

	response := decodeUserModelUsageResponse(t, recorder)
	require.True(t, response.Success)
	require.Equal(t, service.UserModelUsageTotals{TokenUsage: 300, ExternalEvents: 3}, response.Data.Totals)
	require.Len(t, response.Data.Items, 1)
	require.False(t, response.Data.CollectionStatus.DataExportEnabled)
	require.False(t, response.Data.CollectionStatus.ConsumeLogEnabled)
	require.False(t, response.Data.CollectionStatus.ExternalUsageEnabled)
	require.Equal(t, 13, response.Data.CollectionStatus.RefreshIntervalMinutes)
}
