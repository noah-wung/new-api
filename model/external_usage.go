package model

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	ExternalUsageSourceCursor      = "cursor"
	ExternalUsageSourceCodex       = "codex"
	ExternalUsageSourceZCode       = "zcode"
	ExternalUsageSourceMiniMaxCode = "minimax_code"

	ExternalUsageOriginCursorImport = "cursor_import"
	ExternalUsageOriginClientReport = "client_report"

	ExternalUsageStatusActive   = "active"
	ExternalUsageStatusRevoked  = "revoked"
	ExternalUsageStatusReverted = "reverted"
	ExternalUsageStatusAccepted = "accepted"
	ExternalUsageStatusRejected = "rejected"
	ExternalUsageStatusConsumed = "consumed"
)

var ErrExternalUsageDeviceLimitExceeded = errors.New("external usage device limit exceeded")
var ErrExternalUsageCredentialNotFound = errors.New("external usage credential not found")
var ErrExternalUsageInstallTokenNotFound = errors.New("external usage install token not found")
var ErrExternalUsageInstallTokenExpired = errors.New("external usage install token expired")
var ErrExternalUsageInstallTokenConsumed = errors.New("external usage install token already consumed")

type ExternalUsageDevice struct {
	Id                    int    `json:"id"`
	UserID                int    `json:"user_id" gorm:"index:idx_eud_user_status,priority:1;index:uk_eud_user_fingerprint,priority:1"`
	DeviceName            string `json:"device_name" gorm:"type:varchar(128);default:''"`
	DeviceFingerprintHash string `json:"device_fingerprint_hash" gorm:"type:varchar(128);default:'';index:uk_eud_user_fingerprint,priority:2"`
	CredentialPrefix      string `json:"credential_prefix" gorm:"type:varchar(24);default:'';index"`
	CredentialHash        string `json:"-" gorm:"type:varchar(128);uniqueIndex"`
	AllowedSources        string `json:"allowed_sources" gorm:"type:text"`
	Status                string `json:"status" gorm:"type:varchar(32);default:'active';index:idx_eud_user_status,priority:2"`
	LastReportedAt        int64  `json:"last_reported_at" gorm:"bigint"`
	LastSeenAt            int64  `json:"last_seen_at" gorm:"bigint"`
	CreatedAt             int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt             int64  `json:"updated_at" gorm:"bigint"`
}

type ExternalUsageCursorImportBatch struct {
	Id             int    `json:"id"`
	UserID         int    `json:"user_id" gorm:"index"`
	ImportedBy     int    `json:"imported_by" gorm:"index"`
	Source         string `json:"source" gorm:"type:varchar(32);default:'cursor';index"`
	FileName       string `json:"file_name" gorm:"type:varchar(255);default:''"`
	FileSHA256     string `json:"file_sha256" gorm:"type:varchar(64);default:'';index"`
	Status         string `json:"status" gorm:"type:varchar(32);default:'active';index"`
	TotalRows      int    `json:"total_rows" gorm:"default:0"`
	ImportedRows   int    `json:"imported_rows" gorm:"default:0"`
	DuplicateRows  int    `json:"duplicate_rows" gorm:"default:0"`
	RejectedRows   int    `json:"rejected_rows" gorm:"default:0"`
	IgnoredRows    int    `json:"ignored_rows" gorm:"default:0"`
	OccurredFrom   int64  `json:"occurred_from" gorm:"bigint"`
	OccurredTo     int64  `json:"occurred_to" gorm:"bigint"`
	RevertedAt     int64  `json:"reverted_at" gorm:"bigint"`
	OperatorRemark string `json:"operator_remark" gorm:"type:text"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint"`
}

type ExternalUsageCursorEvent struct {
	Id                   int    `json:"id"`
	UserID               int    `json:"user_id" gorm:"index:uk_euce_user_source_event,priority:1;index"`
	BatchID              int    `json:"batch_id" gorm:"index"`
	Source               string `json:"source" gorm:"type:varchar(32);default:'cursor';index:uk_euce_user_source_event,priority:2;index"`
	EventIdentity        string `json:"event_identity" gorm:"type:varchar(128);default:'';index:uk_euce_user_source_event,priority:3"`
	OccurredAt           int64  `json:"occurred_at" gorm:"bigint;index"`
	Status               string `json:"status" gorm:"type:varchar(32);default:'active';index"`
	Kind                 string `json:"kind" gorm:"type:varchar(128);default:''"`
	SourceModelName      string `json:"source_model_name" gorm:"type:varchar(128);default:'';index"`
	NormalizedModelName  string `json:"normalized_model_name" gorm:"type:varchar(128);default:'';index"`
	MaxMode              bool   `json:"max_mode"`
	InputCacheWriteToken int    `json:"input_cache_write_token" gorm:"default:0"`
	InputToken           int    `json:"input_token" gorm:"default:0"`
	CacheReadToken       int    `json:"cache_read_token" gorm:"default:0"`
	OutputToken          int    `json:"output_token" gorm:"default:0"`
	TotalToken           int    `json:"total_token" gorm:"default:0"`
	CostText             string `json:"cost_text" gorm:"type:varchar(64);default:''"`
	CloudAgentHash       string `json:"cloud_agent_hash" gorm:"type:varchar(128);default:''"`
	AutomationHash       string `json:"automation_hash" gorm:"type:varchar(128);default:''"`
	RevertedAt           int64  `json:"reverted_at" gorm:"bigint"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint"`
}

type ExternalUsageReportBatch struct {
	Id             int    `json:"id"`
	UserID         int    `json:"user_id" gorm:"index"`
	DeviceID       int    `json:"device_id" gorm:"index"`
	Source         string `json:"source" gorm:"type:varchar(32);default:'';index"`
	Status         string `json:"status" gorm:"type:varchar(32);default:'accepted';index"`
	ClientVersion  string `json:"client_version" gorm:"type:varchar(64);default:''"`
	AcceptedCount  int    `json:"accepted_count" gorm:"default:0"`
	DuplicateCount int    `json:"duplicate_count" gorm:"default:0"`
	RejectedCount  int    `json:"rejected_count" gorm:"default:0"`
	OccurredFrom   int64  `json:"occurred_from" gorm:"bigint"`
	OccurredTo     int64  `json:"occurred_to" gorm:"bigint"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint"`
}

type ExternalUsageClientEvent struct {
	Id                   int    `json:"id"`
	UserID               int    `json:"user_id" gorm:"index:uk_eucl_user_source_event,priority:1;index"`
	DeviceID             int    `json:"device_id" gorm:"index"`
	BatchID              int    `json:"batch_id" gorm:"index"`
	Source               string `json:"source" gorm:"type:varchar(32);default:'';index:uk_eucl_user_source_event,priority:2;index"`
	EventIdentity        string `json:"event_identity" gorm:"type:varchar(128);default:'';index:uk_eucl_user_source_event,priority:3"`
	OccurredAt           int64  `json:"occurred_at" gorm:"bigint;index"`
	SourceSessionHash    string `json:"source_session_hash" gorm:"type:varchar(128);default:'';index"`
	SourceModelName      string `json:"source_model_name" gorm:"type:varchar(128);default:'';index"`
	NormalizedModelName  string `json:"normalized_model_name" gorm:"type:varchar(128);default:'';index"`
	InputToken           int    `json:"input_token" gorm:"default:0"`
	CachedInputToken     int    `json:"cached_input_token" gorm:"default:0"`
	OutputToken          int    `json:"output_token" gorm:"default:0"`
	ReasoningOutputToken int    `json:"reasoning_output_token" gorm:"default:0"`
	TotalToken           int    `json:"total_token" gorm:"default:0"`
	Status               string `json:"status" gorm:"type:varchar(32);default:'accepted';index"`
	ClientVersion        string `json:"client_version" gorm:"type:varchar(64);default:''"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint"`
}

type ExternalUsageAggregate struct {
	Id                   int    `json:"id"`
	UserID               int    `json:"user_id" gorm:"index:uk_eua_bucket,priority:1;index"`
	Origin               string `json:"origin" gorm:"type:varchar(32);default:'';index:uk_eua_bucket,priority:2;index"`
	Source               string `json:"source" gorm:"type:varchar(32);default:'';index:uk_eua_bucket,priority:3;index"`
	NormalizedModelName  string `json:"normalized_model_name" gorm:"type:varchar(128);default:'';index:uk_eua_bucket,priority:4;index"`
	BucketAt             int64  `json:"bucket_at" gorm:"bigint;index:uk_eua_bucket,priority:5;index"`
	EventCount           int64  `json:"event_count" gorm:"bigint;default:0"`
	InputTokens          int64  `json:"input_tokens" gorm:"bigint;default:0"`
	CachedInputTokens    int64  `json:"cached_input_tokens" gorm:"bigint;default:0"`
	OutputTokens         int64  `json:"output_tokens" gorm:"bigint;default:0"`
	ReasoningOutputToken int64  `json:"reasoning_output_token" gorm:"bigint;default:0"`
	TotalTokens          int64  `json:"total_tokens" gorm:"bigint;default:0"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt            int64  `json:"updated_at" gorm:"bigint"`
}

type ExternalUsageModelMapping struct {
	Id                  int    `json:"id"`
	Source              string `json:"source" gorm:"type:varchar(32);default:'';index:uk_eumm_source_model,priority:1;index"`
	SourceModelName     string `json:"source_model_name" gorm:"type:varchar(128);default:'';index:uk_eumm_source_model,priority:2;index"`
	NormalizedModelName string `json:"normalized_model_name" gorm:"type:varchar(128);default:'';index"`
	Status              string `json:"status" gorm:"type:varchar(32);default:'active';index"`
	CreatedAt           int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt           int64  `json:"updated_at" gorm:"bigint"`
}

type ExternalUsageAggregateDelta struct {
	UserID               int
	Origin               string
	Source               string
	NormalizedModelName  string
	OccurredAt           int64
	EventCount           int64
	InputTokens          int64
	CachedInputTokens    int64
	OutputTokens         int64
	ReasoningOutputToken int64
	TotalTokens          int64
}

type ExternalUsageSummaryRow struct {
	Source               string `json:"source"`
	NormalizedModelName  string `json:"normalized_model_name"`
	TotalTokens          int64  `json:"total_tokens"`
	InputTokens          int64  `json:"input_tokens"`
	CachedInputTokens    int64  `json:"cached_input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	ReasoningOutputToken int64  `json:"reasoning_output_token"`
	EventCount           int64  `json:"event_count"`
}

func (d *ExternalUsageDevice) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	d.CreatedAt = now
	d.UpdatedAt = now
	if d.Status == "" {
		d.Status = ExternalUsageStatusActive
	}
	return nil
}

func (d *ExternalUsageDevice) BeforeUpdate(tx *gorm.DB) error {
	d.UpdatedAt = common.GetTimestamp()
	return nil
}

func (b *ExternalUsageCursorImportBatch) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	b.CreatedAt = now
	b.UpdatedAt = now
	if b.Status == "" {
		b.Status = ExternalUsageStatusActive
	}
	if b.Source == "" {
		b.Source = ExternalUsageSourceCursor
	}
	return nil
}

func (b *ExternalUsageCursorImportBatch) BeforeUpdate(tx *gorm.DB) error {
	b.UpdatedAt = common.GetTimestamp()
	return nil
}

func (e *ExternalUsageCursorEvent) BeforeCreate(tx *gorm.DB) error {
	e.CreatedAt = common.GetTimestamp()
	if e.Source == "" {
		e.Source = ExternalUsageSourceCursor
	}
	if e.Status == "" {
		e.Status = ExternalUsageStatusActive
	}
	return nil
}

func (b *ExternalUsageReportBatch) BeforeCreate(tx *gorm.DB) error {
	b.CreatedAt = common.GetTimestamp()
	if b.Status == "" {
		b.Status = ExternalUsageStatusAccepted
	}
	return nil
}

func (e *ExternalUsageClientEvent) BeforeCreate(tx *gorm.DB) error {
	e.CreatedAt = common.GetTimestamp()
	if e.Status == "" {
		e.Status = ExternalUsageStatusAccepted
	}
	return nil
}

func (a *ExternalUsageAggregate) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	a.CreatedAt = now
	a.UpdatedAt = now
	return nil
}

func (a *ExternalUsageAggregate) BeforeUpdate(tx *gorm.DB) error {
	a.UpdatedAt = common.GetTimestamp()
	return nil
}

func (m *ExternalUsageModelMapping) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	m.CreatedAt = now
	m.UpdatedAt = now
	if m.Status == "" {
		m.Status = ExternalUsageStatusActive
	}
	return nil
}

func (m *ExternalUsageModelMapping) BeforeUpdate(tx *gorm.DB) error {
	m.UpdatedAt = common.GetTimestamp()
	return nil
}

func (d *ExternalUsageDevice) SetAllowedSources(sources []string) error {
	bytes, err := common.Marshal(uniqueNonEmptyStrings(sources))
	if err != nil {
		return err
	}
	d.AllowedSources = string(bytes)
	return nil
}

func (d *ExternalUsageDevice) GetAllowedSources() []string {
	if strings.TrimSpace(d.AllowedSources) == "" {
		return nil
	}
	var sources []string
	if err := common.UnmarshalJsonStr(d.AllowedSources, &sources); err != nil {
		return nil
	}
	return uniqueNonEmptyStrings(sources)
}

func CountActiveExternalUsageDevices(userID int) (int64, error) {
	var count int64
	err := DB.Model(&ExternalUsageDevice{}).
		Where("user_id = ? AND status = ?", userID, ExternalUsageStatusActive).
		Count(&count).Error
	return count, err
}

func CountActiveExternalUsageDevicesTx(tx *gorm.DB, userID int) (int64, error) {
	if tx == nil {
		tx = DB
	}
	var count int64
	err := tx.Model(&ExternalUsageDevice{}).
		Where("user_id = ? AND status = ?", userID, ExternalUsageStatusActive).
		Count(&count).Error
	return count, err
}

func CreateExternalUsageDevice(device *ExternalUsageDevice) error {
	return DB.Create(device).Error
}

func UpdateExternalUsageDevice(device *ExternalUsageDevice) error {
	return DB.Save(device).Error
}

func ListExternalUsageDevicesByUser(userID int) ([]*ExternalUsageDevice, error) {
	var devices []*ExternalUsageDevice
	err := DB.Where("user_id = ?", userID).Order("id DESC").Find(&devices).Error
	return devices, err
}

func GetExternalUsageDeviceByUserAndID(userID int, deviceID int) (*ExternalUsageDevice, error) {
	var device ExternalUsageDevice
	err := DB.Where("user_id = ? AND id = ?", userID, deviceID).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func GetExternalUsageDeviceByFingerprint(userID int, fingerprintHash string) (*ExternalUsageDevice, error) {
	var device ExternalUsageDevice
	err := DB.Where("user_id = ? AND device_fingerprint_hash = ?", userID, fingerprintHash).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func GetExternalUsageDeviceByFingerprintTx(tx *gorm.DB, userID int, fingerprintHash string) (*ExternalUsageDevice, error) {
	if tx == nil {
		tx = DB
	}
	var device ExternalUsageDevice
	err := tx.Where("user_id = ? AND device_fingerprint_hash = ?", userID, fingerprintHash).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func GetExternalUsageDeviceByCredentialHash(credentialHash string) (*ExternalUsageDevice, error) {
	var device ExternalUsageDevice
	err := DB.Where("credential_hash = ? AND status = ?", credentialHash, ExternalUsageStatusActive).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func ExistsExternalUsageClientEventByIdentity(tx *gorm.DB, userID int, source string, eventIdentity string) (bool, error) {
	if tx == nil {
		tx = DB
	}
	var count int64
	err := tx.Model(&ExternalUsageClientEvent{}).
		Where("user_id = ? AND source = ? AND event_identity = ?", userID, strings.TrimSpace(source), strings.TrimSpace(eventIdentity)).
		Count(&count).Error
	return count > 0, err
}

func GetExternalUsageClientEventByIdentityTx(tx *gorm.DB, userID int, source string, eventIdentity string) (*ExternalUsageClientEvent, error) {
	if tx == nil {
		tx = DB
	}
	var event ExternalUsageClientEvent
	if err := tx.Where("user_id = ? AND source = ? AND event_identity = ?", userID, strings.TrimSpace(source), strings.TrimSpace(eventIdentity)).
		First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func FindEquivalentExternalUsageClientEventTx(tx *gorm.DB, userID int, source string, sessionHash string, occurredAt int64, inputToken int, cachedInputToken int, outputToken int, reasoningOutputToken int, totalToken int) (*ExternalUsageClientEvent, error) {
	if tx == nil {
		tx = DB
	}
	var event ExternalUsageClientEvent
	if err := tx.Where(
		"user_id = ? AND source = ? AND occurred_at = ? AND source_session_hash = ? AND input_token = ? AND cached_input_token = ? AND output_token = ? AND reasoning_output_token = ? AND total_token = ?",
		userID,
		strings.TrimSpace(source),
		occurredAt,
		strings.TrimSpace(sessionHash),
		inputToken,
		cachedInputToken,
		outputToken,
		reasoningOutputToken,
		totalToken,
	).Order("id DESC").First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func CreateExternalUsageCursorImportBatch(batch *ExternalUsageCursorImportBatch) error {
	return DB.Create(batch).Error
}

func UpdateExternalUsageCursorImportBatch(batch *ExternalUsageCursorImportBatch) error {
	return DB.Save(batch).Error
}

func ListExternalUsageCursorImportBatches(userID int, isAdmin bool, startIdx int, pageSize int) ([]*ExternalUsageCursorImportBatch, int64, error) {
	var batches []*ExternalUsageCursorImportBatch
	tx := DB.Model(&ExternalUsageCursorImportBatch{})
	if userID > 0 {
		tx = tx.Where("user_id = ?", userID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := tx.Order("id DESC").Offset(startIdx).Limit(pageSize).Find(&batches).Error
	return batches, total, err
}

func CreateExternalUsageReportBatch(batch *ExternalUsageReportBatch) error {
	return DB.Create(batch).Error
}

func ListActiveExternalUsageModelMappings(tx *gorm.DB, source string) ([]ExternalUsageModelMapping, error) {
	if tx == nil {
		tx = DB
	}
	query := tx.Model(&ExternalUsageModelMapping{}).Where("status = ?", ExternalUsageStatusActive)
	if strings.TrimSpace(source) != "" {
		query = query.Where("source = ?", strings.TrimSpace(source))
	}
	var mappings []ExternalUsageModelMapping
	err := query.Order("id ASC").Find(&mappings).Error
	return mappings, err
}

func GetExternalUsageModelMappingByID(tx *gorm.DB, id int) (*ExternalUsageModelMapping, error) {
	if tx == nil {
		tx = DB
	}
	var mapping ExternalUsageModelMapping
	if err := tx.Where("id = ?", id).First(&mapping).Error; err != nil {
		return nil, err
	}
	return &mapping, nil
}

func GetExternalUsageModelMappingBySourceModel(tx *gorm.DB, source string, sourceModelName string) (*ExternalUsageModelMapping, error) {
	if tx == nil {
		tx = DB
	}
	var mapping ExternalUsageModelMapping
	if err := tx.Where("source = ? AND source_model_name = ?", strings.TrimSpace(source), strings.TrimSpace(sourceModelName)).First(&mapping).Error; err != nil {
		return nil, err
	}
	return &mapping, nil
}

type DeleteExternalUsageModelDataResult struct {
	CursorEventsDeleted int64 `json:"cursor_events_deleted"`
	ClientEventsDeleted int64 `json:"client_events_deleted"`
	AggregatesDeleted   int64 `json:"aggregates_deleted"`
}

func DeleteExternalUsageModelData(userID int, source string, normalizedModelName string) (*DeleteExternalUsageModelDataResult, error) {
	result := &DeleteExternalUsageModelDataResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		deleteCursor := tx.Where("user_id = ? AND source = ? AND normalized_model_name = ?", userID, source, normalizedModelName).
			Delete(&ExternalUsageCursorEvent{})
		if deleteCursor.Error != nil {
			return deleteCursor.Error
		}
		result.CursorEventsDeleted = deleteCursor.RowsAffected

		deleteClient := tx.Where("user_id = ? AND source = ? AND normalized_model_name = ?", userID, source, normalizedModelName).
			Delete(&ExternalUsageClientEvent{})
		if deleteClient.Error != nil {
			return deleteClient.Error
		}
		result.ClientEventsDeleted = deleteClient.RowsAffected

		deleteAggregate := tx.Where("user_id = ? AND source = ? AND normalized_model_name = ?", userID, source, normalizedModelName).
			Delete(&ExternalUsageAggregate{})
		if deleteAggregate.Error != nil {
			return deleteAggregate.Error
		}
		result.AggregatesDeleted = deleteAggregate.RowsAffected
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ApplyExternalUsageAggregateDelta(tx *gorm.DB, delta ExternalUsageAggregateDelta) error {
	bucketAt := delta.OccurredAt - (delta.OccurredAt % 3600)
	var row ExternalUsageAggregate
	err := tx.Where(
		"user_id = ? AND origin = ? AND source = ? AND normalized_model_name = ? AND bucket_at = ?",
		delta.UserID,
		delta.Origin,
		delta.Source,
		delta.NormalizedModelName,
		bucketAt,
	).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = ExternalUsageAggregate{
				UserID:               delta.UserID,
				Origin:               delta.Origin,
				Source:               delta.Source,
				NormalizedModelName:  delta.NormalizedModelName,
				BucketAt:             bucketAt,
				EventCount:           delta.EventCount,
				InputTokens:          delta.InputTokens,
				CachedInputTokens:    delta.CachedInputTokens,
				OutputTokens:         delta.OutputTokens,
				ReasoningOutputToken: delta.ReasoningOutputToken,
				TotalTokens:          delta.TotalTokens,
			}
			return tx.Create(&row).Error
		}
		return err
	}
	return tx.Model(&row).Updates(map[string]any{
		"event_count":            gorm.Expr("event_count + ?", delta.EventCount),
		"input_tokens":           gorm.Expr("input_tokens + ?", delta.InputTokens),
		"cached_input_tokens":    gorm.Expr("cached_input_tokens + ?", delta.CachedInputTokens),
		"output_tokens":          gorm.Expr("output_tokens + ?", delta.OutputTokens),
		"reasoning_output_token": gorm.Expr("reasoning_output_token + ?", delta.ReasoningOutputToken),
		"total_tokens":           gorm.Expr("total_tokens + ?", delta.TotalTokens),
		"updated_at":             common.GetTimestamp(),
	}).Error
}

func ListExternalUsageSummaryByUser(userID int, startTime int64, endTime int64) ([]ExternalUsageSummaryRow, error) {
	var rows []ExternalUsageSummaryRow
	tx := DB.Table("external_usage_aggregates").
		Select("source, normalized_model_name, sum(total_tokens) as total_tokens, sum(input_tokens) as input_tokens, sum(cached_input_tokens) as cached_input_tokens, sum(output_tokens) as output_tokens, sum(reasoning_output_token) as reasoning_output_token, sum(event_count) as event_count").
		Where("user_id = ?", userID).
		Group("source, normalized_model_name").
		Order("total_tokens DESC")
	if startTime > 0 {
		tx = tx.Where("bucket_at >= ?", startTime)
	}
	if endTime > 0 {
		tx = tx.Where("bucket_at <= ?", endTime)
	}
	err := tx.Find(&rows).Error
	return rows, err
}

func getExternalQuotaDataByUserID(userID int, startTime int64, endTime int64) ([]*QuotaData, error) {
	var quotaDatas []*QuotaData
	tx := DB.Table("external_usage_aggregates").
		Select("user_id, normalized_model_name as model_name, bucket_at as created_at, sum(event_count) as count, 0 as quota, sum(total_tokens) as token_used").
		Where("user_id = ?", userID).
		Group("user_id, normalized_model_name, bucket_at")
	if startTime > 0 {
		tx = tx.Where("bucket_at >= ?", startTime)
	}
	if endTime > 0 {
		tx = tx.Where("bucket_at <= ?", endTime)
	}
	err := tx.Find(&quotaDatas).Error
	return quotaDatas, err
}

func getExternalQuotaDataGroupByUser(startTime int64, endTime int64) ([]*QuotaData, error) {
	var quotaDatas []*QuotaData
	tx := DB.Table("external_usage_aggregates").
		Select("external_usage_aggregates.user_id, users.username as username, COALESCE(NULLIF(users.display_name, ''), users.username) as display_name, external_usage_aggregates.bucket_at as created_at, sum(external_usage_aggregates.event_count) as count, 0 as quota, sum(external_usage_aggregates.total_tokens) as token_used").
		Joins("LEFT JOIN users ON external_usage_aggregates.user_id = users.id").
		Group("external_usage_aggregates.user_id, users.username, users.display_name, external_usage_aggregates.bucket_at")
	if startTime > 0 {
		tx = tx.Where("external_usage_aggregates.bucket_at >= ?", startTime)
	}
	if endTime > 0 {
		tx = tx.Where("external_usage_aggregates.bucket_at <= ?", endTime)
	}
	err := tx.Find(&quotaDatas).Error
	return quotaDatas, err
}

func getExternalQuotaDataAll(startTime int64, endTime int64) ([]*QuotaData, error) {
	var quotaDatas []*QuotaData
	tx := DB.Table("external_usage_aggregates").
		Select("normalized_model_name as model_name, bucket_at as created_at, sum(event_count) as count, 0 as quota, sum(total_tokens) as token_used").
		Group("normalized_model_name, bucket_at")
	if startTime > 0 {
		tx = tx.Where("bucket_at >= ?", startTime)
	}
	if endTime > 0 {
		tx = tx.Where("bucket_at <= ?", endTime)
	}
	err := tx.Find(&quotaDatas).Error
	return quotaDatas, err
}

func mergeQuotaDataRows(base []*QuotaData, extra []*QuotaData, keyFn func(item *QuotaData) string) []*QuotaData {
	if len(extra) == 0 {
		return base
	}
	if len(base) == 0 {
		return extra
	}
	result := make([]*QuotaData, 0, len(base)+len(extra))
	index := make(map[string]*QuotaData, len(base))
	for _, item := range base {
		clone := *item
		result = append(result, &clone)
		index[keyFn(&clone)] = &clone
	}
	for _, item := range extra {
		key := keyFn(item)
		existing, ok := index[key]
		if !ok {
			clone := *item
			result = append(result, &clone)
			index[key] = &clone
			continue
		}
		existing.Count += item.Count
		existing.Quota += item.Quota
		existing.TokenUsed += item.TokenUsed
		if existing.Username == "" {
			existing.Username = item.Username
		}
		if existing.DisplayName == "" {
			existing.DisplayName = item.DisplayName
		}
	}
	return result
}

func mergeRankingQuotaTotals(base []RankingQuotaTotal, extra []RankingQuotaTotal) []RankingQuotaTotal {
	if len(extra) == 0 {
		return base
	}
	index := make(map[string]int, len(base))
	result := make([]RankingQuotaTotal, 0, len(base)+len(extra))
	for _, item := range base {
		result = append(result, item)
		index[item.ModelName] = len(result) - 1
	}
	for _, item := range extra {
		if idx, ok := index[item.ModelName]; ok {
			result[idx].TotalTokens += item.TotalTokens
			continue
		}
		result = append(result, item)
		index[item.ModelName] = len(result) - 1
	}
	sort.Slice(result, func(i int, j int) bool {
		if result[i].TotalTokens == result[j].TotalTokens {
			return result[i].ModelName < result[j].ModelName
		}
		return result[i].TotalTokens > result[j].TotalTokens
	})
	return result
}

func mergeRankingQuotaBuckets(base []RankingQuotaBucket, extra []RankingQuotaBucket) []RankingQuotaBucket {
	if len(extra) == 0 {
		return base
	}
	index := make(map[string]int, len(base))
	result := make([]RankingQuotaBucket, 0, len(base)+len(extra))
	for _, item := range base {
		result = append(result, item)
		index[item.ModelName+"-"+strconv.FormatInt(item.Bucket, 10)] = len(result) - 1
	}
	for _, item := range extra {
		key := item.ModelName + "-" + strconv.FormatInt(item.Bucket, 10)
		if idx, ok := index[key]; ok {
			result[idx].Tokens += item.Tokens
			continue
		}
		result = append(result, item)
		index[key] = len(result) - 1
	}
	sort.Slice(result, func(i int, j int) bool {
		if result[i].Bucket == result[j].Bucket {
			return result[i].ModelName < result[j].ModelName
		}
		return result[i].Bucket < result[j].Bucket
	})
	return result
}

func getExternalRankingQuotaTotals(startTime int64, endTime int64) ([]RankingQuotaTotal, error) {
	var rows []RankingQuotaTotal
	tx := DB.Table("external_usage_aggregates").
		Select("normalized_model_name as model_name, sum(total_tokens) as total_tokens").
		Where("normalized_model_name <> ''").
		Group("normalized_model_name").
		Having("sum(total_tokens) > 0")
	if startTime > 0 {
		tx = tx.Where("bucket_at >= ?", startTime)
	}
	if endTime > 0 {
		tx = tx.Where("bucket_at <= ?", endTime)
	}
	err := tx.Find(&rows).Error
	return rows, err
}

func getExternalRankingQuotaBuckets(startTime int64, endTime int64, bucketSize int64) ([]RankingQuotaBucket, error) {
	if bucketSize <= 0 {
		bucketSize = 3600
	}
	bucketExpr := rankingExternalBucketExpr(bucketSize)
	var rows []RankingQuotaBucket
	tx := DB.Table("external_usage_aggregates").
		Select("normalized_model_name as model_name, " + bucketExpr + " as bucket, sum(total_tokens) as tokens").
		Where("normalized_model_name <> ''").
		Group("normalized_model_name, " + bucketExpr).
		Having("sum(total_tokens) > 0")
	if startTime > 0 {
		tx = tx.Where("bucket_at >= ?", startTime)
	}
	if endTime > 0 {
		tx = tx.Where("bucket_at <= ?", endTime)
	}
	err := tx.Find(&rows).Error
	return rows, err
}

func rankingExternalBucketExpr(bucketSize int64) string {
	if common.UsingMySQL {
		return "FLOOR(bucket_at / " + strconv.FormatInt(bucketSize, 10) + ") * " + strconv.FormatInt(bucketSize, 10)
	}
	return "(bucket_at / " + strconv.FormatInt(bucketSize, 10) + ") * " + strconv.FormatInt(bucketSize, 10)
}

func uniqueNonEmptyStrings(items []string) []string {
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
