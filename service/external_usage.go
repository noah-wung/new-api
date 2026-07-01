package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"gorm.io/gorm"
)

type IssueExternalUsageDeviceParams struct {
	UserID            int
	DeviceName        string
	DeviceFingerprint string
	AllowedSources    []string
}

type CursorImportResult struct {
	Batch *model.ExternalUsageCursorImportBatch
}

type ClientUsageEventInput struct {
	EventID              string `json:"event_id"`
	SessionID            string `json:"session_id"`
	OccurredAt           int64  `json:"occurred_at"`
	SourceModelName      string `json:"source_model_name"`
	InputTokens          int    `json:"input_tokens"`
	CachedInputTokens    int    `json:"cached_input_tokens"`
	OutputTokens         int    `json:"output_tokens"`
	ReasoningOutputToken int    `json:"reasoning_output_token"`
	TotalTokens          int    `json:"total_tokens"`
}

type ReportExternalUsageParams struct {
	Source        string                  `json:"source"`
	ClientVersion string                  `json:"client_version"`
	Events        []ClientUsageEventInput `json:"events"`
}

type ReportExternalUsageRejected struct {
	EventID string `json:"event_id"`
	Reason  string `json:"reason"`
}

type ReportExternalUsageResult struct {
	Batch              *model.ExternalUsageReportBatch `json:"batch"`
	AcceptedEventIDs   []string                        `json:"accepted_event_ids"`
	DuplicateEventIDs  []string                        `json:"duplicate_event_ids"`
	RejectedEventInfos []ReportExternalUsageRejected   `json:"rejected"`
}

type DeleteExternalUsageModelDataParams struct {
	UserID              int
	Source              string
	NormalizedModelName string
}

type UpsertExternalUsageModelMappingParams struct {
	Source              string
	SourceModelName     string
	NormalizedModelName string
}

type DeleteCursorImportBatchResult struct {
	Batch                 *model.ExternalUsageCursorImportBatch `json:"batch"`
	RevertedEventCount    int64                                 `json:"reverted_event_count"`
	RebuiltAggregateCount int                                   `json:"rebuilt_aggregate_count"`
}

type ReplayCursorImportBatchResult struct {
	Batch                 *model.ExternalUsageCursorImportBatch `json:"batch"`
	ReactivatedEventCount int64                                 `json:"reactivated_event_count"`
	RebuiltAggregateCount int                                   `json:"rebuilt_aggregate_count"`
}

type RebuildExternalUsageAggregatesParams struct {
	UserID    int    `json:"user_id"`
	Source    string `json:"source"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
}

type RebuildExternalUsageAggregatesResult struct {
	DeletedAggregateCount int64  `json:"deleted_aggregate_count"`
	RebuiltAggregateCount int    `json:"rebuilt_aggregate_count"`
	CursorEventCount      int64  `json:"cursor_event_count"`
	ClientEventCount      int64  `json:"client_event_count"`
	Source                string `json:"source"`
	StartBucketAt         int64  `json:"start_bucket_at"`
	EndBucketAt           int64  `json:"end_bucket_at"`
}

type ExternalUsageReportBatchRow struct {
	Id             int    `json:"id"`
	UserID         int    `json:"user_id"`
	DeviceID       int    `json:"device_id"`
	DeviceName     string `json:"device_name"`
	Source         string `json:"source"`
	Status         string `json:"status"`
	ClientVersion  string `json:"client_version"`
	AcceptedCount  int    `json:"accepted_count"`
	DuplicateCount int    `json:"duplicate_count"`
	RejectedCount  int    `json:"rejected_count"`
	OccurredFrom   int64  `json:"occurred_from"`
	OccurredTo     int64  `json:"occurred_to"`
	CreatedAt      int64  `json:"created_at"`
}

type ListExternalUsageDetailsParams struct {
	UserID int
	Source string
	Offset int
	Limit  int
}

type ExternalUsageDetailRow struct {
	Origin               string `json:"origin"`
	Source               string `json:"source"`
	Status               string `json:"status"`
	BatchID              int    `json:"batch_id"`
	DeviceID             int    `json:"device_id"`
	OccurredAt           int64  `json:"occurred_at"`
	EventIdentity        string `json:"event_identity"`
	SourceModelName      string `json:"source_model_name"`
	NormalizedModelName  string `json:"normalized_model_name"`
	InputTokens          int64  `json:"input_tokens"`
	CachedInputTokens    int64  `json:"cached_input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	ReasoningOutputToken int64  `json:"reasoning_output_token"`
	TotalTokens          int64  `json:"total_tokens"`
}

const codexInstallTokenTTL = 15 * time.Minute

type cursorCSVRow struct {
	OccurredAt           int64
	CloudAgentHash       string
	AutomationHash       string
	Kind                 string
	SourceModelName      string
	MaxMode              bool
	InputCacheWriteToken int
	InputToken           int
	CacheReadToken       int
	OutputToken          int
	TotalToken           int
	CostText             string
	EventIdentity        string
	NormalizedModelName  string
	Rejected             bool
	RejectReason         string
}

type externalUsageAggregateKey struct {
	NormalizedModelName string
	BucketAt            int64
}

type externalUsageAggregateSnapshot struct {
	Source               string
	NormalizedModelName  string
	BucketAt             int64
	EventCount           int64
	InputTokens          int64
	CachedInputTokens    int64
	OutputTokens         int64
	ReasoningOutputToken int64
	TotalTokens          int64
}

func IssueExternalUsageDeviceCredential(params IssueExternalUsageDeviceParams) (*model.ExternalUsageDevice, string, error) {
	return issueExternalUsageDeviceCredentialTx(model.DB, params)
}

func issueExternalUsageDeviceCredentialTx(tx *gorm.DB, params IssueExternalUsageDeviceParams) (*model.ExternalUsageDevice, string, error) {
	setting := external_usage_setting.GetSetting()
	allowedSources := normalizeAllowedSources(params.AllowedSources, setting.AllowedSources)
	if len(allowedSources) == 0 {
		allowedSources = append(allowedSources, setting.AllowedSources...)
	}
	deviceName := strings.TrimSpace(params.DeviceName)
	if deviceName == "" {
		deviceName = "Unnamed Device"
	}
	fingerprintHash := sha256Hex(strings.TrimSpace(params.DeviceFingerprint))
	if fingerprintHash == "" {
		return nil, "", errors.New("device fingerprint is required")
	}

	rawToken, err := common.GenerateKey()
	if err != nil {
		return nil, "", err
	}
	rawToken = "eur_" + rawToken
	credentialHash := sha256Hex(rawToken)
	credentialPrefix := rawToken
	if len(credentialPrefix) > 16 {
		credentialPrefix = credentialPrefix[:16]
	}

	var resultDevice *model.ExternalUsageDevice
	err = tx.Transaction(func(tx *gorm.DB) error {
		existing, err := model.GetExternalUsageDeviceByFingerprintTx(tx, params.UserID, fingerprintHash)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existing == nil || errors.Is(err, gorm.ErrRecordNotFound) {
			count, countErr := model.CountActiveExternalUsageDevicesTx(tx, params.UserID)
			if countErr != nil {
				return countErr
			}
			if count >= int64(setting.MaxDevicesPerUser) {
				return model.ErrExternalUsageDeviceLimitExceeded
			}
			device := &model.ExternalUsageDevice{
				UserID:                params.UserID,
				DeviceName:            deviceName,
				DeviceFingerprintHash: fingerprintHash,
				CredentialPrefix:      credentialPrefix,
				CredentialHash:        credentialHash,
				Status:                model.ExternalUsageStatusActive,
			}
			if err := device.SetAllowedSources(allowedSources); err != nil {
				return err
			}
			if err := tx.Create(device).Error; err != nil {
				return err
			}
			resultDevice = device
			return nil
		}
		existing.DeviceName = deviceName
		existing.CredentialPrefix = credentialPrefix
		existing.CredentialHash = credentialHash
		existing.Status = model.ExternalUsageStatusActive
		if err := existing.SetAllowedSources(allowedSources); err != nil {
			return err
		}
		if err := tx.Save(existing).Error; err != nil {
			return err
		}
		resultDevice = existing
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return resultDevice, rawToken, nil
}

func RevokeExternalUsageDevice(userID int, deviceID int) error {
	device, err := model.GetExternalUsageDeviceByUserAndID(userID, deviceID)
	if err != nil {
		return err
	}
	device.Status = model.ExternalUsageStatusRevoked
	return model.UpdateExternalUsageDevice(device)
}

func ImportCursorUsageCSV(adminUserID int, targetUserID int, fileName string, content []byte) (*CursorImportResult, error) {
	setting := external_usage_setting.GetSetting()
	rows, err := parseCursorCSV(content, setting.AcceptWindowDays)
	if err != nil {
		return nil, err
	}
	fileHash := sha256HexBytes(content)
	batch := &model.ExternalUsageCursorImportBatch{
		UserID:     targetUserID,
		ImportedBy: adminUserID,
		FileName:   fileName,
		FileSHA256: fileHash,
		Source:     model.ExternalUsageSourceCursor,
		Status:     model.ExternalUsageStatusActive,
		TotalRows:  len(rows),
	}
	if len(rows) > 0 {
		sort.Slice(rows, func(i int, j int) bool { return rows[i].OccurredAt < rows[j].OccurredAt })
		batch.OccurredFrom = rows[0].OccurredAt
		batch.OccurredTo = rows[len(rows)-1].OccurredAt
	}

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.Rejected {
				batch.RejectedRows++
				continue
			}
			if row.TotalToken <= 0 {
				batch.IgnoredRows++
				continue
			}
			event := &model.ExternalUsageCursorEvent{
				UserID:               targetUserID,
				BatchID:              batch.Id,
				Source:               model.ExternalUsageSourceCursor,
				EventIdentity:        row.EventIdentity,
				OccurredAt:           row.OccurredAt,
				Status:               model.ExternalUsageStatusActive,
				Kind:                 row.Kind,
				SourceModelName:      row.SourceModelName,
				NormalizedModelName:  row.NormalizedModelName,
				MaxMode:              row.MaxMode,
				InputCacheWriteToken: row.InputCacheWriteToken,
				InputToken:           row.InputToken,
				CacheReadToken:       row.CacheReadToken,
				OutputToken:          row.OutputToken,
				TotalToken:           row.TotalToken,
				CostText:             row.CostText,
				CloudAgentHash:       row.CloudAgentHash,
				AutomationHash:       row.AutomationHash,
			}
			if err := tx.Create(event).Error; err != nil {
				if isDuplicateKeyError(err) {
					batch.DuplicateRows++
					continue
				}
				return err
			}
			batch.ImportedRows++
			if err := model.ApplyExternalUsageAggregateDelta(tx, model.ExternalUsageAggregateDelta{
				UserID:              targetUserID,
				Origin:              model.ExternalUsageOriginCursorImport,
				Source:              model.ExternalUsageSourceCursor,
				NormalizedModelName: row.NormalizedModelName,
				OccurredAt:          row.OccurredAt,
				EventCount:          1,
				InputTokens:         int64(row.InputCacheWriteToken + row.InputToken),
				CachedInputTokens:   int64(row.CacheReadToken),
				OutputTokens:        int64(row.OutputToken),
				TotalTokens:         int64(row.TotalToken),
			}); err != nil {
				return err
			}
		}
		return tx.Save(batch).Error
	})
	if err != nil {
		return nil, err
	}
	return &CursorImportResult{Batch: batch}, nil
}

func ReportExternalUsage(device *model.ExternalUsageDevice, params ReportExternalUsageParams) (*ReportExternalUsageResult, error) {
	setting := external_usage_setting.GetSetting()
	source := normalizeSource(params.Source)
	if source == "" {
		return nil, errors.New("unsupported external usage source")
	}
	if err := ValidateExternalUsageSourceEnabled(source); err != nil {
		return nil, err
	}
	if !isSourceAllowed(device.GetAllowedSources(), source) {
		return nil, errors.New("source is not allowed for this credential")
	}

	result := &ReportExternalUsageResult{
		AcceptedEventIDs:   make([]string, 0, len(params.Events)),
		DuplicateEventIDs:  make([]string, 0, len(params.Events)),
		RejectedEventInfos: make([]ReportExternalUsageRejected, 0),
	}
	batch := &model.ExternalUsageReportBatch{
		UserID:        device.UserID,
		DeviceID:      device.Id,
		Source:        source,
		Status:        model.ExternalUsageStatusAccepted,
		ClientVersion: strings.TrimSpace(params.ClientVersion),
	}

	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		modelMappingLookup, err := buildExternalUsageModelMappingLookup(tx, source)
		if err != nil {
			return err
		}
		var acceptedOccurredAt []int64
		for _, eventInput := range params.Events {
			rejectReason := validateClientUsageEvent(eventInput, setting.AcceptWindowDays)
			if rejectReason != "" {
				batch.RejectedCount++
				result.RejectedEventInfos = append(result.RejectedEventInfos, ReportExternalUsageRejected{
					EventID: eventInput.EventID,
					Reason:  rejectReason,
				})
				continue
			}
			sessionHash := strings.TrimSpace(eventInput.SessionID)
			normalizedModelName := normalizeExternalModelNameWithLookup(source, eventInput.SourceModelName, modelMappingLookup)
			existingEvent, err := model.GetExternalUsageClientEventByIdentityTx(tx, device.UserID, source, eventInput.EventID)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if existingEvent == nil && errors.Is(err, gorm.ErrRecordNotFound) {
				existingEvent, err = model.FindEquivalentExternalUsageClientEventTx(
					tx,
					device.UserID,
					source,
					sessionHash,
					eventInput.OccurredAt,
					eventInput.InputTokens,
					eventInput.CachedInputTokens,
					eventInput.OutputTokens,
					eventInput.ReasoningOutputToken,
					eventInput.TotalTokens,
				)
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}
			if existingEvent != nil {
				if err := repairExternalUsageClientEventIfNeeded(tx, existingEvent, source, eventInput, normalizedModelName); err != nil {
					return err
				}
				batch.DuplicateCount++
				result.DuplicateEventIDs = append(result.DuplicateEventIDs, eventInput.EventID)
				acceptedOccurredAt = append(acceptedOccurredAt, eventInput.OccurredAt)
				continue
			}
			event := &model.ExternalUsageClientEvent{
				UserID:               device.UserID,
				DeviceID:             device.Id,
				BatchID:              batch.Id,
				Source:               source,
				EventIdentity:        strings.TrimSpace(eventInput.EventID),
				OccurredAt:           eventInput.OccurredAt,
				SourceSessionHash:    sessionHash,
				SourceModelName:      strings.TrimSpace(eventInput.SourceModelName),
				NormalizedModelName:  normalizedModelName,
				InputToken:           eventInput.InputTokens,
				CachedInputToken:     eventInput.CachedInputTokens,
				OutputToken:          eventInput.OutputTokens,
				ReasoningOutputToken: eventInput.ReasoningOutputToken,
				TotalToken:           eventInput.TotalTokens,
				Status:               model.ExternalUsageStatusAccepted,
				ClientVersion:        strings.TrimSpace(params.ClientVersion),
			}
			if err := tx.Create(event).Error; err != nil {
				if isDuplicateKeyError(err) {
					batch.DuplicateCount++
					result.DuplicateEventIDs = append(result.DuplicateEventIDs, eventInput.EventID)
					acceptedOccurredAt = append(acceptedOccurredAt, eventInput.OccurredAt)
					continue
				}
				return err
			}
			if err := model.ApplyExternalUsageAggregateDelta(tx, model.ExternalUsageAggregateDelta{
				UserID:               device.UserID,
				Origin:               model.ExternalUsageOriginClientReport,
				Source:               source,
				NormalizedModelName:  normalizedModelName,
				OccurredAt:           eventInput.OccurredAt,
				EventCount:           1,
				InputTokens:          int64(eventInput.InputTokens),
				CachedInputTokens:    int64(eventInput.CachedInputTokens),
				OutputTokens:         int64(eventInput.OutputTokens),
				ReasoningOutputToken: int64(eventInput.ReasoningOutputToken),
				TotalTokens:          int64(eventInput.TotalTokens),
			}); err != nil {
				return err
			}
			batch.AcceptedCount++
			result.AcceptedEventIDs = append(result.AcceptedEventIDs, eventInput.EventID)
			acceptedOccurredAt = append(acceptedOccurredAt, eventInput.OccurredAt)
		}
		if len(acceptedOccurredAt) > 0 {
			sort.Slice(acceptedOccurredAt, func(i int, j int) bool { return acceptedOccurredAt[i] < acceptedOccurredAt[j] })
			batch.OccurredFrom = acceptedOccurredAt[0]
			batch.OccurredTo = acceptedOccurredAt[len(acceptedOccurredAt)-1]
			device.LastReportedAt = batch.OccurredTo
		}
		device.LastSeenAt = common.GetTimestamp()
		if err := tx.Save(batch).Error; err != nil {
			return err
		}
		if err := tx.Save(device).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result.Batch = batch
	return result, nil
}

func parseCursorCSV(content []byte, acceptWindowDays int) ([]cursorCSVRow, error) {
	reader := csv.NewReader(bytes.NewReader(content))
	reader.TrimLeadingSpace = true
	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}
	indexByHeader := make(map[string]int, len(headers))
	for idx, header := range headers {
		indexByHeader[strings.TrimSpace(header)] = idx
	}

	now := time.Now()
	cutoff := now.Add(-time.Duration(acceptWindowDays) * 24 * time.Hour).Unix()
	rows := make([]cursorCSVRow, 0)
	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		rawDate := csvField(record, indexByHeader, "Date")
		if strings.TrimSpace(rawDate) == "" {
			continue
		}
		occurredAt, err := parseCursorOccurredAt(rawDate)
		if err != nil {
			return nil, err
		}
		if occurredAt < cutoff || occurredAt > now.Unix()+300 {
			continue
		}
		row := cursorCSVRow{
			OccurredAt:      occurredAt,
			CloudAgentHash:  sha256Hex(csvField(record, indexByHeader, "Cloud Agent ID")),
			AutomationHash:  sha256Hex(csvField(record, indexByHeader, "Automation ID")),
			Kind:            csvField(record, indexByHeader, "Kind"),
			SourceModelName: csvField(record, indexByHeader, "Model"),
			MaxMode:         parseCursorBool(csvField(record, indexByHeader, "Max Mode")),
			CostText:        csvField(record, indexByHeader, "Cost"),
		}
		var parseErr error
		row.InputCacheWriteToken, parseErr = parseCursorIntStrict(csvField(record, indexByHeader, "Input (w/ Cache Write)"))
		if parseErr == nil {
			row.InputToken, parseErr = parseCursorIntStrict(csvField(record, indexByHeader, "Input (w/o Cache Write)"))
		}
		if parseErr == nil {
			row.CacheReadToken, parseErr = parseCursorIntStrict(csvField(record, indexByHeader, "Cache Read"))
		}
		if parseErr == nil {
			row.OutputToken, parseErr = parseCursorIntStrict(csvField(record, indexByHeader, "Output Tokens"))
		}
		if parseErr == nil {
			row.TotalToken, parseErr = parseCursorIntStrict(csvField(record, indexByHeader, "Total Tokens"))
		}
		if parseErr != nil {
			row.Rejected = true
			row.RejectReason = parseErr.Error()
		}
		row.NormalizedModelName = normalizeExternalModelName(model.ExternalUsageSourceCursor, row.SourceModelName)
		row.EventIdentity = cursorEventIdentity(rawDate, record)
		rows = append(rows, row)
	}
	return rows, nil
}

func parseCursorOccurredAt(value string) (int64, error) {
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return ts.Unix(), nil
}

func validateClientUsageEvent(event ClientUsageEventInput, acceptWindowDays int) string {
	if strings.TrimSpace(event.EventID) == "" {
		return "missing event_id"
	}
	if event.OccurredAt <= 0 {
		return "missing occurred_at"
	}
	if event.TotalTokens <= 0 {
		return "total_tokens must be positive"
	}
	if event.InputTokens < 0 {
		return "input_tokens must not be negative"
	}
	if event.CachedInputTokens < 0 {
		return "cached_input_tokens must not be negative"
	}
	if event.OutputTokens < 0 {
		return "output_tokens must not be negative"
	}
	if event.ReasoningOutputToken < 0 {
		return "reasoning_output_token must not be negative"
	}
	now := time.Now()
	cutoff := now.Add(-time.Duration(acceptWindowDays) * 24 * time.Hour).Unix()
	if event.OccurredAt < cutoff {
		return "event is older than allowed window"
	}
	if event.OccurredAt > now.Unix()+300 {
		return "event occurred_at is in the future"
	}
	return ""
}

func csvField(record []string, indexByHeader map[string]int, name string) string {
	idx, ok := indexByHeader[name]
	if !ok || idx < 0 || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

func parseCursorInt(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	intValue, err := strconv.Atoi(value)
	if err == nil {
		return intValue
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int(floatValue)
}

func parseCursorIntStrict(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	intValue, err := strconv.Atoi(value)
	if err == nil {
		return intValue, nil
	}
	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value %q", value)
	}
	return int(floatValue), nil
}

func parseCursorBool(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "yes" || value == "true" || value == "1"
}

func cursorEventIdentity(rawDate string, record []string) string {
	joined := strings.Join(append([]string{rawDate}, record...), "|")
	return sha256Hex(joined)
}

func normalizeAllowedSources(requested []string, allowed []string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		normalized := normalizeSource(item)
		if normalized != "" {
			allowedSet[normalized] = struct{}{}
		}
	}
	result := make([]string, 0, len(requested))
	for _, item := range requested {
		normalized := normalizeSource(item)
		if normalized == "" {
			continue
		}
		if _, ok := allowedSet[normalized]; !ok {
			continue
		}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		for item := range allowedSet {
			result = append(result, item)
		}
		sort.Strings(result)
	}
	return result
}

func isSourceAllowed(allowed []string, source string) bool {
	for _, item := range allowed {
		if normalizeSource(item) == source {
			return true
		}
	}
	return false
}

func normalizeSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case model.ExternalUsageSourceCursor:
		return model.ExternalUsageSourceCursor
	case model.ExternalUsageSourceCodex:
		return model.ExternalUsageSourceCodex
	case model.ExternalUsageSourceZCode:
		return model.ExternalUsageSourceZCode
	case model.ExternalUsageSourceMiniMaxCode:
		return model.ExternalUsageSourceMiniMaxCode
	default:
		return ""
	}
}

func normalizeExternalModelName(source string, sourceModelName string) string {
	return normalizeExternalModelNameWithLookup(source, sourceModelName, nil)
}

func ValidateExternalUsageSourceEnabled(source string) error {
	normalizedSource := normalizeSource(source)
	if normalizedSource == "" {
		return errors.New("unsupported external usage source")
	}
	if !isSourceAllowed(external_usage_setting.GetSetting().AllowedSources, normalizedSource) {
		return fmt.Errorf("external usage source is not enabled: %s", normalizedSource)
	}
	return nil
}

func sha256Hex(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sha256HexBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key value violates unique constraint")
}

func LookupExternalUsageDeviceByCredential(rawCredential string) (*model.ExternalUsageDevice, error) {
	rawCredential = strings.TrimSpace(rawCredential)
	if rawCredential == "" {
		return nil, model.ErrExternalUsageCredentialNotFound
	}
	device, err := model.GetExternalUsageDeviceByCredentialHash(sha256Hex(rawCredential))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.ErrExternalUsageCredentialNotFound
		}
		return nil, err
	}
	return device, nil
}

func BuildExternalUsageOverview(userID int, startTime int64, endTime int64) (map[string]any, error) {
	devices, err := model.ListExternalUsageDevicesByUser(userID)
	if err != nil {
		return nil, err
	}
	summary, err := model.ListExternalUsageSummaryByUser(userID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	var totalTokens int64
	var totalEvents int64
	for _, item := range summary {
		totalTokens += item.TotalTokens
		totalEvents += item.EventCount
	}
	return map[string]any{
		"devices":      devices,
		"summary":      summary,
		"total_tokens": totalTokens,
		"total_events": totalEvents,
	}, nil
}

func ListExternalUsageReportBatches(userID int, startIdx int, pageSize int) ([]ExternalUsageReportBatchRow, int64, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	var batches []model.ExternalUsageReportBatch
	tx := model.DB.Model(&model.ExternalUsageReportBatch{})
	if userID > 0 {
		tx = tx.Where("user_id = ?", userID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := tx.Order("id DESC").Offset(startIdx).Limit(pageSize).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	deviceIDs := make([]int, 0, len(batches))
	for _, batch := range batches {
		if batch.DeviceID > 0 {
			deviceIDs = append(deviceIDs, batch.DeviceID)
		}
	}
	deviceNameByID := make(map[int]string, len(deviceIDs))
	if len(deviceIDs) > 0 {
		var devices []model.ExternalUsageDevice
		if err := model.DB.Where("id IN ?", uniqueInts(deviceIDs)).Find(&devices).Error; err != nil {
			return nil, 0, err
		}
		for _, device := range devices {
			deviceNameByID[device.Id] = device.DeviceName
		}
	}
	rows := make([]ExternalUsageReportBatchRow, 0, len(batches))
	for _, batch := range batches {
		rows = append(rows, ExternalUsageReportBatchRow{
			Id:             batch.Id,
			UserID:         batch.UserID,
			DeviceID:       batch.DeviceID,
			DeviceName:     deviceNameByID[batch.DeviceID],
			Source:         batch.Source,
			Status:         batch.Status,
			ClientVersion:  batch.ClientVersion,
			AcceptedCount:  batch.AcceptedCount,
			DuplicateCount: batch.DuplicateCount,
			RejectedCount:  batch.RejectedCount,
			OccurredFrom:   batch.OccurredFrom,
			OccurredTo:     batch.OccurredTo,
			CreatedAt:      batch.CreatedAt,
		})
	}
	return rows, total, nil
}

func ListExternalUsageDetails(params ListExternalUsageDetailsParams) ([]ExternalUsageDetailRow, int64, error) {
	source := normalizeSource(params.Source)
	if strings.TrimSpace(params.Source) != "" && source == "" {
		return nil, 0, errors.New("unsupported external usage source")
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	fetchLimit := offset + limit

	cursorTotal, err := countCursorUsageDetails(params.UserID, source)
	if err != nil {
		return nil, 0, err
	}
	clientTotal, err := countClientUsageDetails(params.UserID, source)
	if err != nil {
		return nil, 0, err
	}
	cursorRows, err := listCursorUsageDetails(params.UserID, source, fetchLimit)
	if err != nil {
		return nil, 0, err
	}
	clientRows, err := listClientUsageDetails(params.UserID, source, fetchLimit)
	if err != nil {
		return nil, 0, err
	}
	rows := append(cursorRows, clientRows...)
	sort.Slice(rows, func(i int, j int) bool {
		if rows[i].OccurredAt == rows[j].OccurredAt {
			if rows[i].Origin == rows[j].Origin {
				return rows[i].BatchID > rows[j].BatchID
			}
			return rows[i].Origin > rows[j].Origin
		}
		return rows[i].OccurredAt > rows[j].OccurredAt
	})
	total := cursorTotal + clientTotal
	if offset >= len(rows) {
		return []ExternalUsageDetailRow{}, total, nil
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end], total, nil
}

func ListExternalUsageModelMappings(source string) ([]model.ExternalUsageModelMapping, error) {
	normalizedSource := normalizeSource(source)
	if strings.TrimSpace(source) != "" && normalizedSource == "" {
		return nil, errors.New("unsupported external usage source")
	}
	return model.ListActiveExternalUsageModelMappings(model.DB, normalizedSource)
}

func UpsertExternalUsageModelMapping(params UpsertExternalUsageModelMappingParams) (*model.ExternalUsageModelMapping, error) {
	source := normalizeSource(params.Source)
	if source == "" {
		return nil, errors.New("unsupported external usage source")
	}
	sourceModelName := strings.TrimSpace(params.SourceModelName)
	if sourceModelName == "" {
		return nil, errors.New("source_model_name is required")
	}
	normalizedModelName := strings.TrimSpace(params.NormalizedModelName)
	if normalizedModelName == "" {
		return nil, errors.New("normalized_model_name is required")
	}

	var mapping *model.ExternalUsageModelMapping
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		existing, err := model.GetExternalUsageModelMappingBySourceModel(tx, source, sourceModelName)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existing == nil || errors.Is(err, gorm.ErrRecordNotFound) {
			created := &model.ExternalUsageModelMapping{
				Source:              source,
				SourceModelName:     sourceModelName,
				NormalizedModelName: normalizedModelName,
				Status:              model.ExternalUsageStatusActive,
			}
			if err := tx.Create(created).Error; err != nil {
				return err
			}
			mapping = created
			return nil
		}
		existing.NormalizedModelName = normalizedModelName
		existing.Status = model.ExternalUsageStatusActive
		if err := tx.Save(existing).Error; err != nil {
			return err
		}
		mapping = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mapping, nil
}

func DeleteExternalUsageModelMapping(id int) (*model.ExternalUsageModelMapping, error) {
	var mapping *model.ExternalUsageModelMapping
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		existing, err := model.GetExternalUsageModelMappingByID(tx, id)
		if err != nil {
			return err
		}
		existing.Status = model.ExternalUsageStatusRevoked
		if err := tx.Save(existing).Error; err != nil {
			return err
		}
		mapping = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mapping, nil
}

func DeleteExternalUsageModelData(params DeleteExternalUsageModelDataParams) (*model.DeleteExternalUsageModelDataResult, error) {
	source := normalizeSource(params.Source)
	if source == "" {
		return nil, errors.New("unsupported external usage source")
	}
	normalizedModelName := strings.TrimSpace(params.NormalizedModelName)
	if normalizedModelName == "" {
		return nil, errors.New("normalized model name is required")
	}
	return model.DeleteExternalUsageModelData(params.UserID, source, normalizedModelName)
}

func DeleteCursorImportBatch(batchID int) (*DeleteCursorImportBatchResult, error) {
	result := &DeleteCursorImportBatchResult{}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var batch model.ExternalUsageCursorImportBatch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return err
		}
		if batch.Status == model.ExternalUsageStatusReverted {
			return errors.New("cursor import batch is already reverted")
		}

		var events []model.ExternalUsageCursorEvent
		if err := tx.Where("batch_id = ?", batchID).Find(&events).Error; err != nil {
			return err
		}

		affectedBuckets := make(map[externalUsageAggregateKey]struct{}, len(events))
		for _, event := range events {
			affectedBuckets[externalUsageAggregateKey{
				NormalizedModelName: event.NormalizedModelName,
				BucketAt:            bucketAtFromOccurredAt(event.OccurredAt),
			}] = struct{}{}
		}

		updateResult := tx.Model(&model.ExternalUsageCursorEvent{}).
			Where("batch_id = ?", batchID).
			Updates(map[string]any{
				"status":      model.ExternalUsageStatusReverted,
				"reverted_at": common.GetTimestamp(),
			})
		if updateResult.Error != nil {
			return updateResult.Error
		}
		result.RevertedEventCount = updateResult.RowsAffected

		rebuiltCount, err := rebuildCursorAggregateBuckets(tx, batch.UserID, affectedBuckets)
		if err != nil {
			return err
		}
		result.RebuiltAggregateCount = rebuiltCount

		batch.Status = model.ExternalUsageStatusReverted
		batch.RevertedAt = common.GetTimestamp()
		if err := tx.Save(&batch).Error; err != nil {
			return err
		}
		result.Batch = &batch
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ReplayCursorImportBatch(batchID int) (*ReplayCursorImportBatchResult, error) {
	result := &ReplayCursorImportBatchResult{}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var batch model.ExternalUsageCursorImportBatch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return err
		}
		if batch.Status != model.ExternalUsageStatusReverted {
			return errors.New("cursor import batch is not reverted")
		}

		var events []model.ExternalUsageCursorEvent
		if err := tx.Where("batch_id = ?", batchID).Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return errors.New("cursor import batch has no retained facts to replay")
		}

		affectedBuckets := make(map[externalUsageAggregateKey]struct{}, len(events))
		for _, event := range events {
			affectedBuckets[externalUsageAggregateKey{
				NormalizedModelName: event.NormalizedModelName,
				BucketAt:            bucketAtFromOccurredAt(event.OccurredAt),
			}] = struct{}{}
		}

		updateResult := tx.Model(&model.ExternalUsageCursorEvent{}).
			Where("batch_id = ?", batchID).
			Updates(map[string]any{
				"status":      model.ExternalUsageStatusActive,
				"reverted_at": int64(0),
			})
		if updateResult.Error != nil {
			return updateResult.Error
		}
		result.ReactivatedEventCount = updateResult.RowsAffected

		rebuiltCount, err := rebuildCursorAggregateBuckets(tx, batch.UserID, affectedBuckets)
		if err != nil {
			return err
		}
		result.RebuiltAggregateCount = rebuiltCount

		batch.Status = model.ExternalUsageStatusActive
		batch.RevertedAt = 0
		if err := tx.Save(&batch).Error; err != nil {
			return err
		}
		result.Batch = &batch
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func RebuildExternalUsageAggregates(params RebuildExternalUsageAggregatesParams) (*RebuildExternalUsageAggregatesResult, error) {
	source := normalizeSource(params.Source)
	if strings.TrimSpace(params.Source) != "" && source == "" {
		return nil, errors.New("unsupported external usage source")
	}
	startBucketAt, endBucketAt, err := normalizeAggregateRebuildRange(params.StartTime, params.EndTime)
	if err != nil {
		return nil, err
	}

	result := &RebuildExternalUsageAggregatesResult{
		Source:        source,
		StartBucketAt: startBucketAt,
		EndBucketAt:   endBucketAt,
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := refreshExternalUsageNormalizedModelNames(tx, params.UserID, source, startBucketAt, endBucketAt); err != nil {
			return err
		}
		deleteQuery := tx.Where("user_id = ?", params.UserID)
		if source != "" {
			deleteQuery = deleteQuery.Where("source = ?", source)
		}
		if startBucketAt > 0 {
			deleteQuery = deleteQuery.Where("bucket_at >= ?", startBucketAt)
		}
		if endBucketAt > 0 {
			deleteQuery = deleteQuery.Where("bucket_at <= ?", endBucketAt)
		}
		deleteResult := deleteQuery.Delete(&model.ExternalUsageAggregate{})
		if deleteResult.Error != nil {
			return deleteResult.Error
		}
		result.DeletedAggregateCount = deleteResult.RowsAffected

		cursorRows, err := loadCursorAggregateSnapshots(tx, params.UserID, source, startBucketAt, endBucketAt)
		if err != nil {
			return err
		}
		if err := createAggregateRows(tx, params.UserID, model.ExternalUsageOriginCursorImport, cursorRows); err != nil {
			return err
		}

		clientRows, err := loadClientAggregateSnapshots(tx, params.UserID, source, startBucketAt, endBucketAt)
		if err != nil {
			return err
		}
		if err := createAggregateRows(tx, params.UserID, model.ExternalUsageOriginClientReport, clientRows); err != nil {
			return err
		}

		result.RebuiltAggregateCount = len(cursorRows) + len(clientRows)
		for _, row := range cursorRows {
			result.CursorEventCount += row.EventCount
		}
		for _, row := range clientRows {
			result.ClientEventCount += row.EventCount
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func bucketAtFromOccurredAt(occurredAt int64) int64 {
	if occurredAt <= 0 {
		return 0
	}
	return occurredAt - (occurredAt % 3600)
}

func normalizeAggregateRebuildRange(startTime int64, endTime int64) (int64, int64, error) {
	if startTime > 0 && endTime > 0 && startTime > endTime {
		return 0, 0, errors.New("start_time must be less than or equal to end_time")
	}
	return bucketAtFromOccurredAt(startTime), bucketAtFromOccurredAt(endTime), nil
}

func rebuildCursorAggregateBuckets(tx *gorm.DB, userID int, keys map[externalUsageAggregateKey]struct{}) (int, error) {
	rebuiltCount := 0
	for key := range keys {
		var snapshot externalUsageAggregateSnapshot
		if err := tx.Model(&model.ExternalUsageCursorEvent{}).
			Select("count(*) as event_count, coalesce(sum(input_cache_write_token + input_token), 0) as input_tokens, coalesce(sum(cache_read_token), 0) as cached_input_tokens, coalesce(sum(output_token), 0) as output_tokens, 0 as reasoning_output_token, coalesce(sum(total_token), 0) as total_tokens").
			Where("user_id = ? AND source = ? AND status = ? AND normalized_model_name = ? AND occurred_at >= ? AND occurred_at < ?",
				userID,
				model.ExternalUsageSourceCursor,
				model.ExternalUsageStatusActive,
				key.NormalizedModelName,
				key.BucketAt,
				key.BucketAt+3600,
			).
			Scan(&snapshot).Error; err != nil {
			return 0, err
		}

		aggregateFilter := tx.Where(
			"user_id = ? AND origin = ? AND source = ? AND normalized_model_name = ? AND bucket_at = ?",
			userID,
			model.ExternalUsageOriginCursorImport,
			model.ExternalUsageSourceCursor,
			key.NormalizedModelName,
			key.BucketAt,
		)
		if snapshot.EventCount == 0 {
			if err := aggregateFilter.Delete(&model.ExternalUsageAggregate{}).Error; err != nil {
				return 0, err
			}
			continue
		}

		var aggregate model.ExternalUsageAggregate
		err := aggregateFilter.First(&aggregate).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				aggregate = model.ExternalUsageAggregate{
					UserID:              userID,
					Origin:              model.ExternalUsageOriginCursorImport,
					Source:              model.ExternalUsageSourceCursor,
					NormalizedModelName: key.NormalizedModelName,
					BucketAt:            key.BucketAt,
				}
			} else {
				return 0, err
			}
		}
		aggregate.EventCount = snapshot.EventCount
		aggregate.InputTokens = snapshot.InputTokens
		aggregate.CachedInputTokens = snapshot.CachedInputTokens
		aggregate.OutputTokens = snapshot.OutputTokens
		aggregate.ReasoningOutputToken = snapshot.ReasoningOutputToken
		aggregate.TotalTokens = snapshot.TotalTokens

		if aggregate.Id == 0 {
			if err := tx.Create(&aggregate).Error; err != nil {
				return 0, err
			}
		} else {
			if err := tx.Save(&aggregate).Error; err != nil {
				return 0, err
			}
		}
		rebuiltCount++
	}
	return rebuiltCount, nil
}

func rebuildClientAggregateBuckets(tx *gorm.DB, userID int, source string, keys map[externalUsageAggregateKey]struct{}) (int, error) {
	rebuiltCount := 0
	for key := range keys {
		var snapshot externalUsageAggregateSnapshot
		if err := tx.Model(&model.ExternalUsageClientEvent{}).
			Select("count(*) as event_count, coalesce(sum(input_token), 0) as input_tokens, coalesce(sum(cached_input_token), 0) as cached_input_tokens, coalesce(sum(output_token), 0) as output_tokens, coalesce(sum(reasoning_output_token), 0) as reasoning_output_token, coalesce(sum(total_token), 0) as total_tokens").
			Where("user_id = ? AND source = ? AND status = ? AND normalized_model_name = ? AND occurred_at >= ? AND occurred_at < ?",
				userID,
				source,
				model.ExternalUsageStatusAccepted,
				key.NormalizedModelName,
				key.BucketAt,
				key.BucketAt+3600,
			).
			Scan(&snapshot).Error; err != nil {
			return 0, err
		}

		aggregateFilter := tx.Where(
			"user_id = ? AND origin = ? AND source = ? AND normalized_model_name = ? AND bucket_at = ?",
			userID,
			model.ExternalUsageOriginClientReport,
			source,
			key.NormalizedModelName,
			key.BucketAt,
		)
		if snapshot.EventCount == 0 {
			if err := aggregateFilter.Delete(&model.ExternalUsageAggregate{}).Error; err != nil {
				return 0, err
			}
			continue
		}

		var aggregate model.ExternalUsageAggregate
		err := aggregateFilter.First(&aggregate).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				aggregate = model.ExternalUsageAggregate{
					UserID:              userID,
					Origin:              model.ExternalUsageOriginClientReport,
					Source:              source,
					NormalizedModelName: key.NormalizedModelName,
					BucketAt:            key.BucketAt,
				}
			} else {
				return 0, err
			}
		}
		aggregate.EventCount = snapshot.EventCount
		aggregate.InputTokens = snapshot.InputTokens
		aggregate.CachedInputTokens = snapshot.CachedInputTokens
		aggregate.OutputTokens = snapshot.OutputTokens
		aggregate.ReasoningOutputToken = snapshot.ReasoningOutputToken
		aggregate.TotalTokens = snapshot.TotalTokens

		if aggregate.Id == 0 {
			if err := tx.Create(&aggregate).Error; err != nil {
				return 0, err
			}
		} else {
			if err := tx.Save(&aggregate).Error; err != nil {
				return 0, err
			}
		}
		rebuiltCount++
	}
	return rebuiltCount, nil
}

func loadCursorAggregateSnapshots(tx *gorm.DB, userID int, source string, startBucketAt int64, endBucketAt int64) ([]externalUsageAggregateSnapshot, error) {
	if source != "" && source != model.ExternalUsageSourceCursor {
		return nil, nil
	}
	var rows []externalUsageAggregateSnapshot
	query := tx.Model(&model.ExternalUsageCursorEvent{}).
		Select("source, normalized_model_name, occurred_at - (occurred_at % 3600) as bucket_at, count(*) as event_count, coalesce(sum(input_cache_write_token + input_token), 0) as input_tokens, coalesce(sum(cache_read_token), 0) as cached_input_tokens, coalesce(sum(output_token), 0) as output_tokens, 0 as reasoning_output_token, coalesce(sum(total_token), 0) as total_tokens").
		Where("user_id = ? AND status = ?", userID, model.ExternalUsageStatusActive).
		Group("source, normalized_model_name, occurred_at - (occurred_at % 3600)")
	if startBucketAt > 0 {
		query = query.Where("occurred_at >= ?", startBucketAt)
	}
	if endBucketAt > 0 {
		query = query.Where("occurred_at < ?", endBucketAt+3600)
	}
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func loadClientAggregateSnapshots(tx *gorm.DB, userID int, source string, startBucketAt int64, endBucketAt int64) ([]externalUsageAggregateSnapshot, error) {
	var rows []externalUsageAggregateSnapshot
	query := tx.Model(&model.ExternalUsageClientEvent{}).
		Select("source, normalized_model_name, occurred_at - (occurred_at % 3600) as bucket_at, count(*) as event_count, coalesce(sum(input_token), 0) as input_tokens, coalesce(sum(cached_input_token), 0) as cached_input_tokens, coalesce(sum(output_token), 0) as output_tokens, coalesce(sum(reasoning_output_token), 0) as reasoning_output_token, coalesce(sum(total_token), 0) as total_tokens").
		Where("user_id = ? AND status = ?", userID, model.ExternalUsageStatusAccepted).
		Group("source, normalized_model_name, occurred_at - (occurred_at % 3600)")
	if startBucketAt > 0 {
		query = query.Where("occurred_at >= ?", startBucketAt)
	}
	if endBucketAt > 0 {
		query = query.Where("occurred_at < ?", endBucketAt+3600)
	}
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func createAggregateRows(tx *gorm.DB, userID int, origin string, rows []externalUsageAggregateSnapshot) error {
	for _, row := range rows {
		aggregate := model.ExternalUsageAggregate{
			UserID:               userID,
			Origin:               origin,
			Source:               row.Source,
			NormalizedModelName:  row.NormalizedModelName,
			BucketAt:             row.BucketAt,
			EventCount:           row.EventCount,
			InputTokens:          row.InputTokens,
			CachedInputTokens:    row.CachedInputTokens,
			OutputTokens:         row.OutputTokens,
			ReasoningOutputToken: row.ReasoningOutputToken,
			TotalTokens:          row.TotalTokens,
		}
		if err := tx.Create(&aggregate).Error; err != nil {
			return err
		}
	}
	return nil
}

func listCursorUsageDetails(userID int, source string, size int) ([]ExternalUsageDetailRow, error) {
	if source != "" && source != model.ExternalUsageSourceCursor {
		return nil, nil
	}
	var events []model.ExternalUsageCursorEvent
	query := model.DB.Where("user_id = ?", userID).Order("occurred_at DESC, id DESC").Limit(size)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}
	rows := make([]ExternalUsageDetailRow, 0, len(events))
	for _, event := range events {
		rows = append(rows, ExternalUsageDetailRow{
			Origin:               model.ExternalUsageOriginCursorImport,
			Source:               event.Source,
			Status:               event.Status,
			BatchID:              event.BatchID,
			OccurredAt:           event.OccurredAt,
			EventIdentity:        event.EventIdentity,
			SourceModelName:      event.SourceModelName,
			NormalizedModelName:  event.NormalizedModelName,
			InputTokens:          int64(event.InputCacheWriteToken + event.InputToken),
			CachedInputTokens:    int64(event.CacheReadToken),
			OutputTokens:         int64(event.OutputToken),
			ReasoningOutputToken: 0,
			TotalTokens:          int64(event.TotalToken),
		})
	}
	return rows, nil
}

func countCursorUsageDetails(userID int, source string) (int64, error) {
	if source != "" && source != model.ExternalUsageSourceCursor {
		return 0, nil
	}
	var total int64
	query := model.DB.Model(&model.ExternalUsageCursorEvent{}).Where("user_id = ?", userID)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func listClientUsageDetails(userID int, source string, size int) ([]ExternalUsageDetailRow, error) {
	var events []model.ExternalUsageClientEvent
	query := model.DB.Where("user_id = ?", userID).Order("occurred_at DESC, id DESC").Limit(size)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if err := query.Find(&events).Error; err != nil {
		return nil, err
	}
	rows := make([]ExternalUsageDetailRow, 0, len(events))
	for _, event := range events {
		rows = append(rows, ExternalUsageDetailRow{
			Origin:               model.ExternalUsageOriginClientReport,
			Source:               event.Source,
			Status:               event.Status,
			BatchID:              event.BatchID,
			DeviceID:             event.DeviceID,
			OccurredAt:           event.OccurredAt,
			EventIdentity:        event.EventIdentity,
			SourceModelName:      event.SourceModelName,
			NormalizedModelName:  event.NormalizedModelName,
			InputTokens:          int64(event.InputToken),
			CachedInputTokens:    int64(event.CachedInputToken),
			OutputTokens:         int64(event.OutputToken),
			ReasoningOutputToken: int64(event.ReasoningOutputToken),
			TotalTokens:          int64(event.TotalToken),
		})
	}
	return rows, nil
}

func countClientUsageDetails(userID int, source string) (int64, error) {
	var total int64
	query := model.DB.Model(&model.ExternalUsageClientEvent{}).Where("user_id = ?", userID)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func uniqueInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeExternalModelNameWithLookup(source string, sourceModelName string, lookup map[string]string) string {
	source = strings.TrimSpace(source)
	sourceModelName = strings.TrimSpace(sourceModelName)
	if normalized, ok := resolveExternalModelMapping(source, sourceModelName, lookup); ok {
		return normalized
	}
	if sourceModelName == "" {
		return source + ":unknown"
	}
	return source + ":" + sourceModelName
}

func isUnknownExternalUsageSourceModelName(sourceModelName string) bool {
	sourceModelName = strings.TrimSpace(sourceModelName)
	return sourceModelName == "" || strings.EqualFold(sourceModelName, "unknown")
}

func repairExternalUsageClientEventIfNeeded(tx *gorm.DB, existingEvent *model.ExternalUsageClientEvent, source string, incoming ClientUsageEventInput, normalizedModelName string) error {
	if tx == nil || existingEvent == nil {
		return nil
	}
	incomingModelName := strings.TrimSpace(incoming.SourceModelName)
	if incomingModelName == "" {
		return nil
	}
	if !isUnknownExternalUsageSourceModelName(existingEvent.SourceModelName) {
		return nil
	}

	incomingSessionHash := strings.TrimSpace(incoming.SessionID)
	updates := map[string]any{
		"source_model_name":     incomingModelName,
		"normalized_model_name": normalizedModelName,
	}
	if strings.TrimSpace(existingEvent.SourceSessionHash) == "" && incomingSessionHash != "" {
		updates["source_session_hash"] = incomingSessionHash
	}
	if err := tx.Model(&model.ExternalUsageClientEvent{}).
		Where("id = ?", existingEvent.Id).
		Updates(updates).Error; err != nil {
		return err
	}

	keys := map[externalUsageAggregateKey]struct{}{
		{
			NormalizedModelName: existingEvent.NormalizedModelName,
			BucketAt:            bucketAtFromOccurredAt(existingEvent.OccurredAt),
		}: {},
		{
			NormalizedModelName: normalizedModelName,
			BucketAt:            bucketAtFromOccurredAt(existingEvent.OccurredAt),
		}: {},
	}
	if _, err := rebuildClientAggregateBuckets(tx, existingEvent.UserID, source, keys); err != nil {
		return err
	}

	existingEvent.SourceModelName = incomingModelName
	existingEvent.NormalizedModelName = normalizedModelName
	if strings.TrimSpace(existingEvent.SourceSessionHash) == "" && incomingSessionHash != "" {
		existingEvent.SourceSessionHash = incomingSessionHash
	}
	return nil
}

func resolveExternalModelMapping(source string, sourceModelName string, lookup map[string]string) (string, bool) {
	key := externalUsageModelMappingKey(source, sourceModelName)
	if key == "" {
		return "", false
	}
	if lookup != nil {
		normalized, ok := lookup[key]
		return normalized, ok
	}
	if model.DB == nil {
		return "", false
	}
	mappings, err := model.ListActiveExternalUsageModelMappings(model.DB, source)
	if err != nil {
		return "", false
	}
	for _, item := range mappings {
		if externalUsageModelMappingKey(item.Source, item.SourceModelName) == key {
			normalized := strings.TrimSpace(item.NormalizedModelName)
			if normalized != "" {
				return normalized, true
			}
			return "", false
		}
	}
	return "", false
}

func buildExternalUsageModelMappingLookup(tx *gorm.DB, source string) (map[string]string, error) {
	mappings, err := model.ListActiveExternalUsageModelMappings(tx, source)
	if err != nil {
		return nil, err
	}
	lookup := make(map[string]string, len(mappings))
	for _, item := range mappings {
		key := externalUsageModelMappingKey(item.Source, item.SourceModelName)
		if key == "" {
			continue
		}
		normalized := strings.TrimSpace(item.NormalizedModelName)
		if normalized == "" {
			continue
		}
		lookup[key] = normalized
	}
	return lookup, nil
}

func externalUsageModelMappingKey(source string, sourceModelName string) string {
	source = strings.TrimSpace(strings.ToLower(source))
	sourceModelName = strings.TrimSpace(sourceModelName)
	if source == "" || sourceModelName == "" {
		return ""
	}
	return source + "\x00" + sourceModelName
}

func refreshExternalUsageNormalizedModelNames(tx *gorm.DB, userID int, source string, startBucketAt int64, endBucketAt int64) error {
	lookup, err := buildExternalUsageModelMappingLookup(tx, source)
	if err != nil {
		return err
	}
	if err := refreshCursorNormalizedModelNames(tx, userID, source, startBucketAt, endBucketAt, lookup); err != nil {
		return err
	}
	if err := refreshClientNormalizedModelNames(tx, userID, source, startBucketAt, endBucketAt, lookup); err != nil {
		return err
	}
	return nil
}

func refreshCursorNormalizedModelNames(tx *gorm.DB, userID int, source string, startBucketAt int64, endBucketAt int64, lookup map[string]string) error {
	if source != "" && source != model.ExternalUsageSourceCursor {
		return nil
	}
	var events []model.ExternalUsageCursorEvent
	query := tx.Where("user_id = ? AND status = ?", userID, model.ExternalUsageStatusActive)
	if startBucketAt > 0 {
		query = query.Where("occurred_at >= ?", startBucketAt)
	}
	if endBucketAt > 0 {
		query = query.Where("occurred_at < ?", endBucketAt+3600)
	}
	if err := query.Find(&events).Error; err != nil {
		return err
	}
	for _, event := range events {
		normalized := normalizeExternalModelNameWithLookup(event.Source, event.SourceModelName, lookup)
		if normalized == event.NormalizedModelName {
			continue
		}
		if err := tx.Model(&model.ExternalUsageCursorEvent{}).
			Where("id = ?", event.Id).
			Update("normalized_model_name", normalized).Error; err != nil {
			return err
		}
	}
	return nil
}

func refreshClientNormalizedModelNames(tx *gorm.DB, userID int, source string, startBucketAt int64, endBucketAt int64, lookup map[string]string) error {
	var events []model.ExternalUsageClientEvent
	query := tx.Where("user_id = ? AND status = ?", userID, model.ExternalUsageStatusAccepted)
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if startBucketAt > 0 {
		query = query.Where("occurred_at >= ?", startBucketAt)
	}
	if endBucketAt > 0 {
		query = query.Where("occurred_at < ?", endBucketAt+3600)
	}
	if err := query.Find(&events).Error; err != nil {
		return err
	}
	for _, event := range events {
		normalized := normalizeExternalModelNameWithLookup(event.Source, event.SourceModelName, lookup)
		if normalized == event.NormalizedModelName {
			continue
		}
		if err := tx.Model(&model.ExternalUsageClientEvent{}).
			Where("id = ?", event.Id).
			Update("normalized_model_name", normalized).Error; err != nil {
			return err
		}
	}
	return nil
}

func BuildExternalUsageCredentialDisplay(rawToken string) string {
	if len(rawToken) <= 8 {
		return rawToken
	}
	return rawToken[:8] + "..."
}

func FormatDuplicateDeviceLimit(maxDevices int) error {
	return fmt.Errorf("only %d devices are supported per user", maxDevices)
}
