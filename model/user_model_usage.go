package model

import (
	"sort"
	"strings"
)

type GatewayUserModelUsageRow struct {
	ModelName       string `json:"model_name"`
	TokenUsage      int64  `json:"token_usage"`
	Quota           int64  `json:"quota"`
	GatewayRequests int64  `json:"gateway_requests"`
}

type ExternalUserModelUsageRow struct {
	Source         string `json:"source"`
	ModelName      string `json:"model_name"`
	TokenUsage     int64  `json:"token_usage"`
	ExternalEvents int64  `json:"external_events"`
}

func GetUserModelUsageTarget(userID int) (*User, error) {
	var user User
	err := DB.Unscoped().
		Select("id, username, display_name, deleted_at").
		First(&user, "id = ?", userID).Error
	return &user, err
}

func GetGatewayUserModelUsageRows(userID int, startTime int64, endTime int64) ([]GatewayUserModelUsageRow, error) {
	var rows []GatewayUserModelUsageRow
	err := DB.Table("quota_data").
		Select("model_name, sum(token_used) as token_usage, sum(quota) as quota, sum(count) as gateway_requests").
		Where("user_id = ? AND created_at >= ? AND created_at <= ?", userID, startTime, endTime).
		Group("model_name").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	folded := make(map[string]GatewayUserModelUsageRow, len(rows))
	for _, row := range rows {
		modelName := strings.ToLower(row.ModelName)
		current := folded[modelName]
		current.ModelName = modelName
		current.TokenUsage += row.TokenUsage
		current.Quota += row.Quota
		current.GatewayRequests += row.GatewayRequests
		folded[modelName] = current
	}

	modelNames := make([]string, 0, len(folded))
	for modelName := range folded {
		modelNames = append(modelNames, modelName)
	}
	sort.Strings(modelNames)

	result := make([]GatewayUserModelUsageRow, 0, len(modelNames))
	for _, modelName := range modelNames {
		result = append(result, folded[modelName])
	}
	return result, nil
}

func GetExternalUserModelUsageRows(userID int, startTime int64, endTime int64) ([]ExternalUserModelUsageRow, error) {
	var rows []ExternalUserModelUsageRow
	err := DB.Table("external_usage_aggregates").
		Select("source, normalized_model_name as model_name, sum(total_tokens) as token_usage, sum(event_count) as external_events").
		Where("user_id = ? AND bucket_at >= ? AND bucket_at <= ?", userID, startTime, endTime).
		Group("source, normalized_model_name").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	type externalUsageKey struct {
		source    string
		modelName string
	}
	folded := make(map[externalUsageKey]ExternalUserModelUsageRow, len(rows))
	for _, row := range rows {
		key := externalUsageKey{source: row.Source, modelName: strings.ToLower(row.ModelName)}
		current := folded[key]
		current.Source = key.source
		current.ModelName = key.modelName
		current.TokenUsage += row.TokenUsage
		current.ExternalEvents += row.ExternalEvents
		folded[key] = current
	}

	keys := make([]externalUsageKey, 0, len(folded))
	for key := range folded {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i int, j int) bool {
		if keys[i].source == keys[j].source {
			return keys[i].modelName < keys[j].modelName
		}
		return keys[i].source < keys[j].source
	})

	result := make([]ExternalUserModelUsageRow, 0, len(keys))
	for _, key := range keys {
		result = append(result, folded[key])
	}
	return result, nil
}
