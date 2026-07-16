package model

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
	return rows, err
}

func GetExternalUserModelUsageRows(userID int, startTime int64, endTime int64) ([]ExternalUserModelUsageRow, error) {
	var rows []ExternalUserModelUsageRow
	err := DB.Table("external_usage_aggregates").
		Select("source, normalized_model_name as model_name, sum(total_tokens) as token_usage, sum(event_count) as external_events").
		Where("user_id = ? AND bucket_at >= ? AND bucket_at <= ?", userID, startTime, endTime).
		Group("source, normalized_model_name").
		Find(&rows).Error
	return rows, err
}
