package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/model"
)

func TestBuildUserModelUsagePageMergesSourcesAndKeepsMetricsSeparate(t *testing.T) {
	target := &model.User{Id: 7, Username: "alice", DisplayName: "Alice", Status: 1}
	gateway := []model.GatewayUserModelUsageRow{
		{ModelName: "gpt-5", TokenUsage: 100, Quota: 100, GatewayRequests: 4},
		{ModelName: "GPT-5", TokenUsage: 50, Quota: 25, GatewayRequests: 2},
		{ModelName: "Foo%_Bar", TokenUsage: 10},
	}
	external := []model.ExternalUserModelUsageRow{
		{Source: "codex", ModelName: "GPT-5", TokenUsage: 300, ExternalEvents: 2},
		{Source: "cursor", ModelName: "gpt-5", TokenUsage: 100, ExternalEvents: 1},
		{Source: "cursor", ModelName: "cursor:auto", TokenUsage: 200, ExternalEvents: 3},
	}

	page := BuildUserModelUsagePage(target, gateway, external, nil, UserModelUsageQuery{
		Sources:   []string{"gateway", "codex", "cursor"},
		SortBy:    UserModelUsageSortTokenUsage,
		SortOrder: UserModelUsageSortDesc,
		Page:      1,
		PageSize:  2,
	})

	require.Equal(t, UserModelUsageTarget{
		ID: 7, Username: "alice", DisplayName: "Alice", Status: 1, Deleted: false,
	}, page.User)
	require.Equal(t, []string{"gateway", "codex", "cursor"}, page.AvailableSources)
	require.Equal(t, UserModelUsageTotals{
		TokenUsage: 760, Quota: 125, GatewayRequests: 6, ExternalEvents: 6,
	}, page.Totals)
	require.Equal(t, 3, page.Total)
	require.Equal(t, 1, page.Page)
	require.Equal(t, 2, page.PageSize)
	require.Len(t, page.Items, 2)
	require.Equal(t, "gpt-5", page.Items[0].ModelName)
	require.False(t, page.Items[0].Unmapped)
	require.Equal(t, int64(550), page.Items[0].TokenUsage)
	require.Equal(t, int64(125), page.Items[0].Quota)
	require.Equal(t, int64(6), page.Items[0].GatewayRequests)
	require.Equal(t, int64(3), page.Items[0].ExternalEvents)
	require.Equal(t, []string{"gateway", "codex", "cursor"}, []string{
		page.Items[0].Sources[0].Source,
		page.Items[0].Sources[1].Source,
		page.Items[0].Sources[2].Source,
	})
	require.NotNil(t, page.Items[0].Sources[0].Quota)
	require.Equal(t, int64(125), *page.Items[0].Sources[0].Quota)
	require.NotNil(t, page.Items[0].Sources[0].GatewayRequests)
	require.Nil(t, page.Items[0].Sources[0].ExternalEvents)
	require.Nil(t, page.Items[0].Sources[1].Quota)
	require.Nil(t, page.Items[0].Sources[1].GatewayRequests)
	require.NotNil(t, page.Items[0].Sources[1].ExternalEvents)
	require.Equal(t, int64(2), *page.Items[0].Sources[1].ExternalEvents)
	require.Equal(t, "cursor:auto", page.Items[1].ModelName)
	require.True(t, page.Items[1].Unmapped)
}

func TestBuildUserModelUsagePageFiltersSourcesBeforeTotalsAndSearchesLiterally(t *testing.T) {
	gateway := []model.GatewayUserModelUsageRow{
		{ModelName: "Foo%_Bar", TokenUsage: 10, Quota: 8, GatewayRequests: 1},
		{ModelName: "FooXXBar", TokenUsage: 20, Quota: 16, GatewayRequests: 2},
	}
	external := []model.ExternalUserModelUsageRow{
		{Source: "cursor", ModelName: "FOO%_BAR", TokenUsage: 30, ExternalEvents: 3},
		{Source: "codex", ModelName: "other", TokenUsage: 40, ExternalEvents: 4},
	}

	page := BuildUserModelUsagePage(&model.User{}, gateway, external, nil, UserModelUsageQuery{
		Sources:     []string{" CURSOR "},
		ModelSearch: "foo%_bar",
		SortBy:      UserModelUsageSortModelName,
		SortOrder:   UserModelUsageSortAsc,
		Page:        1,
		PageSize:    20,
	})

	require.Equal(t, []string{"gateway", "codex", "cursor"}, page.AvailableSources)
	require.Equal(t, UserModelUsageTotals{TokenUsage: 30, ExternalEvents: 3}, page.Totals)
	require.Equal(t, 1, page.Total)
	require.Equal(t, "foo%_bar", page.Items[0].ModelName)
	require.Equal(t, []UserModelUsageSource{{
		Source: "cursor", TokenUsage: 30, ExternalEvents: int64Pointer(3),
	}}, page.Items[0].Sources)
}

func TestBuildUserModelUsagePageTreatsEmptyNormalizedSourceFilterAsAll(t *testing.T) {
	page := BuildUserModelUsagePage(
		&model.User{},
		[]model.GatewayUserModelUsageRow{{ModelName: "gpt-5", TokenUsage: 10}},
		[]model.ExternalUserModelUsageRow{{Source: "codex", ModelName: "gpt-5", TokenUsage: 20}},
		nil,
		UserModelUsageQuery{Sources: []string{"", "  "}, Page: 1, PageSize: 20},
	)

	require.Equal(t, UserModelUsageTotals{TokenUsage: 30}, page.Totals)
	require.Equal(t, []string{"gateway", "codex"}, page.AvailableSources)
}

func TestBuildUserModelUsagePageUsesActiveMappingTargetsForFallbackNames(t *testing.T) {
	external := []model.ExternalUserModelUsageRow{
		{Source: "cursor", ModelName: "CURSOR:AUTO", TokenUsage: 30, ExternalEvents: 1},
		{Source: "codex", ModelName: "codex:unknown", TokenUsage: 20, ExternalEvents: 1},
	}
	mappings := []model.ExternalUsageModelMapping{
		{Source: "CURSOR", NormalizedModelName: "Cursor:Auto", Status: model.ExternalUsageStatusActive},
		{Source: "codex", NormalizedModelName: "codex:unknown", Status: "disabled"},
	}

	page := BuildUserModelUsagePage(&model.User{}, nil, external, mappings, UserModelUsageQuery{
		Page: 1, PageSize: 20,
	})

	require.Equal(t, 2, page.Total)
	require.Equal(t, "codex:unknown", page.Items[0].ModelName)
	require.True(t, page.Items[0].Unmapped)
	require.Equal(t, "cursor:auto", page.Items[1].ModelName)
	require.False(t, page.Items[1].Unmapped)
}

func TestBuildUserModelUsagePageSortsNumericFieldsWithModelNameTieBreak(t *testing.T) {
	gateway := []model.GatewayUserModelUsageRow{
		{ModelName: "beta", TokenUsage: 20, Quota: 5, GatewayRequests: 2},
		{ModelName: "alpha", TokenUsage: 20, Quota: 5, GatewayRequests: 2},
		{ModelName: "gamma", TokenUsage: 10, Quota: 9, GatewayRequests: 1},
	}
	external := []model.ExternalUserModelUsageRow{
		{Source: "codex", ModelName: "beta", ExternalEvents: 4},
		{Source: "codex", ModelName: "alpha", ExternalEvents: 4},
		{Source: "codex", ModelName: "gamma", ExternalEvents: 8},
	}

	tests := []struct {
		name      string
		sortBy    string
		sortOrder string
		want      []string
	}{
		{name: "token descending", sortBy: UserModelUsageSortTokenUsage, sortOrder: UserModelUsageSortDesc, want: []string{"alpha", "beta", "gamma"}},
		{name: "quota ascending", sortBy: UserModelUsageSortQuota, sortOrder: UserModelUsageSortAsc, want: []string{"alpha", "beta", "gamma"}},
		{name: "gateway requests descending", sortBy: UserModelUsageSortGatewayRequests, sortOrder: UserModelUsageSortDesc, want: []string{"alpha", "beta", "gamma"}},
		{name: "external events descending", sortBy: UserModelUsageSortExternalEvents, sortOrder: UserModelUsageSortDesc, want: []string{"gamma", "alpha", "beta"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := BuildUserModelUsagePage(&model.User{}, gateway, external, nil, UserModelUsageQuery{
				SortBy: tt.sortBy, SortOrder: tt.sortOrder, Page: 1, PageSize: 20,
			})
			modelNames := make([]string, 0, len(page.Items))
			for _, item := range page.Items {
				modelNames = append(modelNames, item.ModelName)
			}
			require.Equal(t, tt.want, modelNames)
		})
	}
}

func TestBuildUserModelUsagePagePaginatesAndReturnsNonNilEmptySlices(t *testing.T) {
	gateway := []model.GatewayUserModelUsageRow{
		{ModelName: "alpha", TokenUsage: 30},
		{ModelName: "beta", TokenUsage: 20},
		{ModelName: "gamma", TokenUsage: 10},
	}

	page := BuildUserModelUsagePage(&model.User{}, gateway, nil, nil, UserModelUsageQuery{
		SortBy: UserModelUsageSortTokenUsage, SortOrder: UserModelUsageSortDesc, Page: 2, PageSize: 2,
	})
	require.Equal(t, 3, page.Total)
	require.Equal(t, []UserModelUsageItem{{
		ModelName:  "gamma",
		TokenUsage: 10,
		Sources: []UserModelUsageSource{{
			Source: UserModelUsageSourceGateway, TokenUsage: 10,
			Quota: int64Pointer(0), GatewayRequests: int64Pointer(0),
		}},
	}}, page.Items)

	empty := BuildUserModelUsagePage(&model.User{}, nil, nil, nil, UserModelUsageQuery{Page: 1, PageSize: 20})
	require.NotNil(t, empty.AvailableSources)
	require.NotNil(t, empty.Items)
	require.Empty(t, empty.AvailableSources)
	require.Empty(t, empty.Items)
}

func int64Pointer(value int64) *int64 {
	return &value
}
