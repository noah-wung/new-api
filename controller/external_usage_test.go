package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

type externalUsageDetailsPage struct {
	Page     int                              `json:"page"`
	PageSize int                              `json:"page_size"`
	Total    int                              `json:"total"`
	Items    []serviceExternalUsageDetailItem `json:"items"`
}

type serviceExternalUsageDetailItem struct {
	Origin              string `json:"origin"`
	Source              string `json:"source"`
	Status              string `json:"status"`
	NormalizedModelName string `json:"normalized_model_name"`
	TotalTokens         int64  `json:"total_tokens"`
	OccurredAt          int64  `json:"occurred_at"`
}

func enableExternalUsageForTest(t *testing.T) {
	t.Helper()

	raw := config.GlobalConfig.Get("external_usage_setting")
	cfg, ok := raw.(*external_usage_setting.ExternalUsageSetting)
	if !ok || cfg == nil {
		t.Fatal("external_usage_setting config is not registered")
	}
	previous := *cfg
	cfg.Enabled = true
	t.Cleanup(func() {
		*cfg = previous
	})
}

func setupExternalUsageControllerTestDB(t *testing.T) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	if err := db.AutoMigrate(
		&model.ExternalUsageCursorEvent{},
		&model.ExternalUsageClientEvent{},
		&model.ExternalUsageModelMapping{},
	); err != nil {
		t.Fatalf("failed to migrate external usage tables: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM external_usage_model_mappings")
		db.Exec("DELETE FROM external_usage_cursor_events")
		db.Exec("DELETE FROM external_usage_client_events")
	})
}

func performAdminExternalUsageDetailsRequest(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("external-usage-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", "root")
		session.Set("role", common.RoleRootUser)
		session.Set("id", 1)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		if err := session.Save(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false})
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/external-usage/admin/details", middleware.AdminAuth(), ListExternalUsageDetails)

	loginRecorder := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	router.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusNoContent {
		t.Fatalf("loginRecorder.Code = %d, want %d", loginRecorder.Code, http.StatusNoContent)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("New-Api-User", "1")
	for _, item := range loginRecorder.Result().Cookies() {
		request.AddCookie(item)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestListExternalUsageDetails_AdminReturnsPagedRowsForSelectedUser(t *testing.T) {
	setupExternalUsageControllerTestDB(t)
	enableExternalUsageForTest(t)

	rows := []*model.ExternalUsageCursorEvent{
		{
			UserID:              1,
			BatchID:             10,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusActive,
			EventIdentity:       "root-1",
			OccurredAt:          1717200001,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          10,
			CacheReadToken:      20,
			OutputToken:         5,
			TotalToken:          35,
		},
		{
			UserID:              2,
			BatchID:             20,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusActive,
			EventIdentity:       "test-1",
			OccurredAt:          1717200100,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          1,
			CacheReadToken:      2,
			OutputToken:         3,
			TotalToken:          6,
		},
		{
			UserID:              2,
			BatchID:             20,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusActive,
			EventIdentity:       "test-2",
			OccurredAt:          1717200200,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          4,
			CacheReadToken:      5,
			OutputToken:         6,
			TotalToken:          15,
		},
		{
			UserID:              2,
			BatchID:             20,
			Source:              model.ExternalUsageSourceCursor,
			Status:              model.ExternalUsageStatusActive,
			EventIdentity:       "test-3",
			OccurredAt:          1717200300,
			SourceModelName:     "auto",
			NormalizedModelName: "cursor:auto",
			InputToken:          7,
			CacheReadToken:      8,
			OutputToken:         9,
			TotalToken:          24,
		},
	}
	for _, row := range rows {
		if err := model.DB.Create(row).Error; err != nil {
			t.Fatalf("Create(%s) error: %v", row.EventIdentity, err)
		}
	}

	recorder := performAdminExternalUsageDetailsRequest(t, "/api/external-usage/admin/details?user_id=2&p=1&size=2")
	if recorder.Code != http.StatusOK {
		t.Fatalf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response tokenAPIResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode API response: %v", err)
	}
	if !response.Success {
		t.Fatalf("response.Success = false, message=%q", response.Message)
	}

	var page externalUsageDetailsPage
	if err := common.Unmarshal(response.Data, &page); err != nil {
		t.Fatalf("failed to decode details page: %v", err)
	}
	if page.Page != 1 {
		t.Fatalf("page.Page = %d, want 1", page.Page)
	}
	if page.PageSize != 2 {
		t.Fatalf("page.PageSize = %d, want 2", page.PageSize)
	}
	if page.Total != 3 {
		t.Fatalf("page.Total = %d, want 3", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(page.Items) = %d, want 2", len(page.Items))
	}
	if page.Items[0].OccurredAt != 1717200300 {
		t.Fatalf("page.Items[0].OccurredAt = %d, want 1717200300", page.Items[0].OccurredAt)
	}
	if page.Items[0].TotalTokens != 24 {
		t.Fatalf("page.Items[0].TotalTokens = %d, want 24", page.Items[0].TotalTokens)
	}
	if page.Items[0].Origin != model.ExternalUsageOriginCursorImport {
		t.Fatalf("page.Items[0].Origin = %q, want %q", page.Items[0].Origin, model.ExternalUsageOriginCursorImport)
	}
	if page.Items[1].OccurredAt != 1717200200 {
		t.Fatalf("page.Items[1].OccurredAt = %d, want 1717200200", page.Items[1].OccurredAt)
	}

	pageTwoRecorder := performAdminExternalUsageDetailsRequest(t, "/api/external-usage/admin/details?user_id=2&p=2&size=2")
	if pageTwoRecorder.Code != http.StatusOK {
		t.Fatalf("pageTwoRecorder.Code = %d, want %d", pageTwoRecorder.Code, http.StatusOK)
	}
	if err := common.Unmarshal(pageTwoRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode API response for page 2: %v", err)
	}
	if !response.Success {
		t.Fatalf("page 2 response.Success = false, message=%q", response.Message)
	}
	if err := common.Unmarshal(response.Data, &page); err != nil {
		t.Fatalf("failed to decode page 2 details page: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("page 2 total = %d, want 3", page.Total)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(page 2 items) = %d, want 1", len(page.Items))
	}
	if page.Items[0].OccurredAt != 1717200100 {
		t.Fatalf("page.Items[0].OccurredAt on page 2 = %d, want 1717200100", page.Items[0].OccurredAt)
	}
}

func TestExternalUsageModelMappings_AdminCRUD(t *testing.T) {
	setupExternalUsageControllerTestDB(t)
	enableExternalUsageForTest(t)

	createCtx, createRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/external-usage/admin/model-mappings", map[string]any{
		"source":                model.ExternalUsageSourceCursor,
		"source_model_name":     "auto",
		"normalized_model_name": "gateway:gpt-5",
	}, 1)
	createCtx.Set("role", common.RoleRootUser)

	UpsertExternalUsageModelMapping(createCtx)

	if createRecorder.Code != http.StatusOK {
		t.Fatalf("createRecorder.Code = %d, want %d", createRecorder.Code, http.StatusOK)
	}
	createResponse := decodeAPIResponse(t, createRecorder)
	if !createResponse.Success {
		t.Fatalf("createResponse.Success = false, message=%q", createResponse.Message)
	}

	listCtx, listRecorder := newAuthenticatedContext(t, http.MethodGet, "/api/external-usage/admin/model-mappings?source=cursor", nil, 1)
	listCtx.Set("role", common.RoleRootUser)

	ListExternalUsageModelMappings(listCtx)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("listRecorder.Code = %d, want %d", listRecorder.Code, http.StatusOK)
	}
	listResponse := decodeAPIResponse(t, listRecorder)
	if !listResponse.Success {
		t.Fatalf("listResponse.Success = false, message=%q", listResponse.Message)
	}

	var rows []model.ExternalUsageModelMapping
	if err := common.Unmarshal(listResponse.Data, &rows); err != nil {
		t.Fatalf("failed to decode mapping list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].NormalizedModelName != "gateway:gpt-5" {
		t.Fatalf("rows[0].NormalizedModelName = %q, want %q", rows[0].NormalizedModelName, "gateway:gpt-5")
	}

	updateCtx, updateRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/external-usage/admin/model-mappings", map[string]any{
		"source":                model.ExternalUsageSourceCursor,
		"source_model_name":     "auto",
		"normalized_model_name": "gateway:gpt-5-high",
	}, 1)
	updateCtx.Set("role", common.RoleRootUser)

	UpsertExternalUsageModelMapping(updateCtx)

	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("updateRecorder.Code = %d, want %d", updateRecorder.Code, http.StatusOK)
	}

	var mappingCount int64
	if err := model.DB.Model(&model.ExternalUsageModelMapping{}).Count(&mappingCount).Error; err != nil {
		t.Fatalf("Count(model mappings) error: %v", err)
	}
	if mappingCount != 1 {
		t.Fatalf("mappingCount = %d, want 1", mappingCount)
	}

	var refreshed model.ExternalUsageModelMapping
	if err := model.DB.First(&refreshed, rows[0].Id).Error; err != nil {
		t.Fatalf("First(mapping) error: %v", err)
	}
	if refreshed.NormalizedModelName != "gateway:gpt-5-high" {
		t.Fatalf("refreshed.NormalizedModelName = %q, want %q", refreshed.NormalizedModelName, "gateway:gpt-5-high")
	}

	deleteCtx, deleteRecorder := newAuthenticatedContext(t, http.MethodDelete, "/api/external-usage/admin/model-mappings/1", nil, 1)
	deleteCtx.Set("role", common.RoleRootUser)
	deleteCtx.Params = gin.Params{{Key: "id", Value: "1"}}

	DeleteExternalUsageModelMapping(deleteCtx)

	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("deleteRecorder.Code = %d, want %d", deleteRecorder.Code, http.StatusOK)
	}
	if err := model.DB.First(&refreshed, rows[0].Id).Error; err != nil {
		t.Fatalf("First(mapping after delete) error: %v", err)
	}
	if refreshed.Status != model.ExternalUsageStatusRevoked {
		t.Fatalf("refreshed.Status = %q, want %q", refreshed.Status, model.ExternalUsageStatusRevoked)
	}
}
