package service

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/model"
)

const (
	UserModelUsageSourceGateway = "gateway"
	UserModelUsageSourceCursor  = model.ExternalUsageSourceCursor
	UserModelUsageSourceCodex   = model.ExternalUsageSourceCodex

	UserModelUsageSortModelName       = "model_name"
	UserModelUsageSortTokenUsage      = "token_usage"
	UserModelUsageSortQuota           = "quota"
	UserModelUsageSortGatewayRequests = "gateway_requests"
	UserModelUsageSortExternalEvents  = "external_events"

	UserModelUsageSortAsc  = "asc"
	UserModelUsageSortDesc = "desc"
)

type UserModelUsageQuery struct {
	UserID      int
	StartTime   int64
	EndTime     int64
	Sources     []string
	ModelSearch string
	SortBy      string
	SortOrder   string
	Page        int
	PageSize    int
}

type UserModelUsageTarget struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Status      int    `json:"status"`
	Deleted     bool   `json:"deleted"`
}

type UserModelUsageTotals struct {
	TokenUsage      int64 `json:"token_usage"`
	Quota           int64 `json:"quota"`
	GatewayRequests int64 `json:"gateway_requests"`
	ExternalEvents  int64 `json:"external_events"`
}

type UserModelUsageSource struct {
	Source          string `json:"source"`
	TokenUsage      int64  `json:"token_usage"`
	Quota           *int64 `json:"quota"`
	GatewayRequests *int64 `json:"gateway_requests"`
	ExternalEvents  *int64 `json:"external_events"`
}

type UserModelUsageItem struct {
	ModelName       string                 `json:"model_name"`
	Unmapped        bool                   `json:"unmapped"`
	TokenUsage      int64                  `json:"token_usage"`
	Quota           int64                  `json:"quota"`
	GatewayRequests int64                  `json:"gateway_requests"`
	ExternalEvents  int64                  `json:"external_events"`
	Sources         []UserModelUsageSource `json:"sources"`
}

type UserModelUsagePage struct {
	User             UserModelUsageTarget `json:"user"`
	AvailableSources []string             `json:"available_sources"`
	Totals           UserModelUsageTotals `json:"totals"`
	Items            []UserModelUsageItem `json:"items"`
	Page             int                  `json:"page"`
	PageSize         int                  `json:"page_size"`
	Total            int                  `json:"total"`
}

type userModelUsageSourceAggregate struct {
	tokenUsage      int64
	quota           int64
	gatewayRequests int64
	externalEvents  int64
}

type userModelUsageAggregate struct {
	modelName string
	unmapped  bool
	sources   map[string]*userModelUsageSourceAggregate
}

func GetUserModelUsage(params UserModelUsageQuery) (*UserModelUsagePage, error) {
	target, err := model.GetUserModelUsageTarget(params.UserID)
	if err != nil {
		return nil, err
	}
	gatewayRows, err := model.GetGatewayUserModelUsageRows(params.UserID, params.StartTime, params.EndTime)
	if err != nil {
		return nil, err
	}
	externalRows, err := model.GetExternalUserModelUsageRows(params.UserID, params.StartTime, params.EndTime)
	if err != nil {
		return nil, err
	}
	mappings, err := model.ListActiveExternalUsageModelMappings(model.DB, "")
	if err != nil {
		return nil, err
	}
	return BuildUserModelUsagePage(target, gatewayRows, externalRows, mappings, params), nil
}

func BuildUserModelUsagePage(
	target *model.User,
	gatewayRows []model.GatewayUserModelUsageRow,
	externalRows []model.ExternalUserModelUsageRow,
	mappings []model.ExternalUsageModelMapping,
	query UserModelUsageQuery,
) *UserModelUsagePage {
	selectedSources := normalizeUserModelUsageSources(query.Sources)
	availableSources := availableUserModelUsageSources(gatewayRows, externalRows)
	mappingTargets := activeUserModelUsageMappingTargets(mappings)
	aggregates := make(map[string]*userModelUsageAggregate)

	if sourceSelected(selectedSources, UserModelUsageSourceGateway) {
		for _, row := range gatewayRows {
			modelName := strings.ToLower(row.ModelName)
			aggregate := userModelUsageAggregateFor(aggregates, modelName)
			source := userModelUsageSourceFor(aggregate, UserModelUsageSourceGateway)
			source.tokenUsage += row.TokenUsage
			source.quota += row.Quota
			source.gatewayRequests += row.GatewayRequests
		}
	}

	for _, row := range externalRows {
		sourceName := normalizeUserModelUsageSource(row.Source)
		if !sourceSelected(selectedSources, sourceName) {
			continue
		}
		modelName := strings.ToLower(row.ModelName)
		aggregate := userModelUsageAggregateFor(aggregates, modelName)
		source := userModelUsageSourceFor(aggregate, sourceName)
		source.tokenUsage += row.TokenUsage
		source.externalEvents += row.ExternalEvents
		if strings.HasPrefix(modelName, sourceName+":") && !mappingTargets[sourceName][modelName] {
			aggregate.unmapped = true
		}
	}

	search := strings.ToLower(query.ModelSearch)
	items := make([]UserModelUsageItem, 0, len(aggregates))
	totals := UserModelUsageTotals{}
	for _, aggregate := range aggregates {
		if !strings.Contains(aggregate.modelName, search) {
			continue
		}
		item := materializeUserModelUsageItem(aggregate)
		totals.TokenUsage += item.TokenUsage
		totals.Quota += item.Quota
		totals.GatewayRequests += item.GatewayRequests
		totals.ExternalEvents += item.ExternalEvents
		items = append(items, item)
	}

	sortUserModelUsageItems(items, query.SortBy, query.SortOrder)
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	pagedItems := make([]UserModelUsageItem, end-start)
	copy(pagedItems, items[start:end])

	return &UserModelUsagePage{
		User:             materializeUserModelUsageTarget(target),
		AvailableSources: availableSources,
		Totals:           totals,
		Items:            pagedItems,
		Page:             page,
		PageSize:         pageSize,
		Total:            total,
	}
}

func normalizeUserModelUsageSources(sources []string) map[string]bool {
	if len(sources) == 0 {
		return nil
	}
	normalized := make(map[string]bool, len(sources))
	for _, source := range sources {
		source = normalizeUserModelUsageSource(source)
		if source != "" {
			normalized[source] = true
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeUserModelUsageSource(source string) string {
	return strings.ToLower(strings.TrimSpace(source))
}

func sourceSelected(selected map[string]bool, source string) bool {
	return selected == nil || selected[source]
}

func availableUserModelUsageSources(gatewayRows []model.GatewayUserModelUsageRow, externalRows []model.ExternalUserModelUsageRow) []string {
	externalSources := make(map[string]bool)
	for _, row := range externalRows {
		source := normalizeUserModelUsageSource(row.Source)
		if source != "" && source != UserModelUsageSourceGateway {
			externalSources[source] = true
		}
	}
	externalSourceNames := make([]string, 0, len(externalSources))
	for source := range externalSources {
		externalSourceNames = append(externalSourceNames, source)
	}
	sort.Strings(externalSourceNames)

	result := make([]string, 0, len(externalSources)+1)
	if len(gatewayRows) > 0 {
		result = append(result, UserModelUsageSourceGateway)
	}
	result = append(result, externalSourceNames...)
	return result
}

func activeUserModelUsageMappingTargets(mappings []model.ExternalUsageModelMapping) map[string]map[string]bool {
	targets := make(map[string]map[string]bool)
	for _, mapping := range mappings {
		if normalizeUserModelUsageSource(mapping.Status) != model.ExternalUsageStatusActive {
			continue
		}
		source := normalizeUserModelUsageSource(mapping.Source)
		modelName := strings.ToLower(mapping.NormalizedModelName)
		if targets[source] == nil {
			targets[source] = make(map[string]bool)
		}
		targets[source][modelName] = true
	}
	return targets
}

func userModelUsageAggregateFor(aggregates map[string]*userModelUsageAggregate, modelName string) *userModelUsageAggregate {
	aggregate := aggregates[modelName]
	if aggregate == nil {
		aggregate = &userModelUsageAggregate{
			modelName: modelName,
			sources:   make(map[string]*userModelUsageSourceAggregate),
		}
		aggregates[modelName] = aggregate
	}
	return aggregate
}

func userModelUsageSourceFor(aggregate *userModelUsageAggregate, sourceName string) *userModelUsageSourceAggregate {
	source := aggregate.sources[sourceName]
	if source == nil {
		source = &userModelUsageSourceAggregate{}
		aggregate.sources[sourceName] = source
	}
	return source
}

func materializeUserModelUsageTarget(target *model.User) UserModelUsageTarget {
	if target == nil {
		return UserModelUsageTarget{}
	}
	return UserModelUsageTarget{
		ID:          target.Id,
		Username:    target.Username,
		DisplayName: target.DisplayName,
		Status:      target.Status,
		Deleted:     target.DeletedAt.Valid,
	}
}

func materializeUserModelUsageItem(aggregate *userModelUsageAggregate) UserModelUsageItem {
	sourceNames := make([]string, 0, len(aggregate.sources))
	for sourceName := range aggregate.sources {
		sourceNames = append(sourceNames, sourceName)
	}
	sort.Slice(sourceNames, func(i int, j int) bool {
		if sourceNames[i] == UserModelUsageSourceGateway {
			return true
		}
		if sourceNames[j] == UserModelUsageSourceGateway {
			return false
		}
		return sourceNames[i] < sourceNames[j]
	})

	item := UserModelUsageItem{
		ModelName: aggregate.modelName,
		Unmapped:  aggregate.unmapped,
		Sources:   make([]UserModelUsageSource, 0, len(sourceNames)),
	}
	for _, sourceName := range sourceNames {
		aggregatedSource := aggregate.sources[sourceName]
		source := UserModelUsageSource{
			Source:     sourceName,
			TokenUsage: aggregatedSource.tokenUsage,
		}
		if sourceName == UserModelUsageSourceGateway {
			source.Quota = userModelUsageInt64Pointer(aggregatedSource.quota)
			source.GatewayRequests = userModelUsageInt64Pointer(aggregatedSource.gatewayRequests)
		} else {
			source.ExternalEvents = userModelUsageInt64Pointer(aggregatedSource.externalEvents)
		}
		item.TokenUsage += aggregatedSource.tokenUsage
		item.Quota += aggregatedSource.quota
		item.GatewayRequests += aggregatedSource.gatewayRequests
		item.ExternalEvents += aggregatedSource.externalEvents
		item.Sources = append(item.Sources, source)
	}
	return item
}

func userModelUsageInt64Pointer(value int64) *int64 {
	return &value
}

func sortUserModelUsageItems(items []UserModelUsageItem, sortBy string, sortOrder string) {
	sortBy = strings.ToLower(sortBy)
	sortOrder = strings.ToLower(sortOrder)
	sort.Slice(items, func(i int, j int) bool {
		left := items[i]
		right := items[j]
		if sortBy == UserModelUsageSortModelName || sortBy == "" {
			if sortOrder == UserModelUsageSortDesc {
				return left.ModelName > right.ModelName
			}
			return left.ModelName < right.ModelName
		}

		leftValue := userModelUsageSortValue(left, sortBy)
		rightValue := userModelUsageSortValue(right, sortBy)
		if leftValue == rightValue {
			return left.ModelName < right.ModelName
		}
		if sortOrder == UserModelUsageSortAsc {
			return leftValue < rightValue
		}
		return leftValue > rightValue
	})
}

func userModelUsageSortValue(item UserModelUsageItem, sortBy string) int64 {
	switch sortBy {
	case UserModelUsageSortQuota:
		return item.Quota
	case UserModelUsageSortGatewayRequests:
		return item.GatewayRequests
	case UserModelUsageSortExternalEvents:
		return item.ExternalEvents
	default:
		return item.TokenUsage
	}
}
