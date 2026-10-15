package service

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
	"gorm.io/gorm"
)

type ConsumeClaudeInstallTokenParams struct {
	InstallToken        string
	DeviceName          string
	DeviceFingerprint   string
	ReportingCredential string
}

type ConsumeClaudeInstallTokenResult struct {
	Device              *model.ExternalUsageDevice
	ReportingCredential string
	UpstreamURL         string
	UpstreamIsGateway   bool
}

func IssueClaudeInstallToken(userID int, upstream string, upstreamIsGateway bool) (*model.ExternalUsageInstallToken, string, error) {
	origin, err := usagereporter.NormalizeClaudeUpstreamURL(upstream)
	if err != nil || len(origin) > 512 {
		return nil, "", errors.New("invalid Claude upstream URL")
	}
	if err := ValidateExternalUsageSourceEnabled(model.ExternalUsageSourceClaudeCode); err != nil {
		return nil, "", err
	}
	key, err := common.GenerateKey()
	if err != nil {
		return nil, "", err
	}
	raw := "eut_" + key
	token := &model.ExternalUsageInstallToken{
		UserID: userID, Source: model.ExternalUsageSourceClaudeCode,
		UpstreamURL: origin, UpstreamIsGateway: upstreamIsGateway,
		TokenPrefix: raw[:16], TokenHash: sha256Hex(raw),
		Status:    model.ExternalUsageStatusActive,
		ExpiresAt: common.GetTimestamp() + int64(codexInstallTokenTTL/time.Second),
	}
	if err := model.CreateExternalUsageInstallToken(nil, token); err != nil {
		return nil, "", err
	}
	return token, raw, nil
}

func ValidateClaudeInstallToken(raw string) (*model.ExternalUsageInstallToken, error) {
	return validateActiveUsageInstallTokenTx(model.DB, raw, model.ExternalUsageSourceClaudeCode)
}

func ConsumeClaudeInstallToken(params ConsumeClaudeInstallTokenParams) (*ConsumeClaudeInstallTokenResult, error) {
	result := &ConsumeClaudeInstallTokenResult{}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		token, err := validateActiveUsageInstallTokenTx(tx, params.InstallToken, model.ExternalUsageSourceClaudeCode)
		if err != nil {
			return err
		}
		// Claim atomically before issuing a credential. A failure later in this
		// transaction rolls the claim back, including a device-limit failure.
		claimed := tx.Model(&model.ExternalUsageInstallToken{}).
			Where("id = ? AND status = ? AND consumed_at = ?", token.Id, model.ExternalUsageStatusActive, 0).
			Updates(map[string]any{"status": model.ExternalUsageStatusConsumed, "consumed_at": common.GetTimestamp(), "updated_at": common.GetTimestamp()})
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected != 1 {
			return model.ErrExternalUsageInstallTokenConsumed
		}
		previousCredential := strings.TrimSpace(params.ReportingCredential)
		if previousCredential != "" {
			var previous model.ExternalUsageDevice
			err := tx.Where("credential_hash = ? AND user_id = ? AND device_fingerprint_hash = ? AND status = ?", sha256Hex(previousCredential), token.UserID, sha256Hex(strings.TrimSpace(params.DeviceFingerprint)), model.ExternalUsageStatusActive).First(&previous).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err != nil || !isSourceAllowed(previous.GetAllowedSources(), model.ExternalUsageSourceClaudeCode) {
				return errors.New("existing installation identity must be resolved before reinstalling")
			}
			if err == nil && isSourceAllowed(previous.GetAllowedSources(), model.ExternalUsageSourceClaudeCode) {
				result.Device = &previous
				result.ReportingCredential = previousCredential
			}
		}
		if result.Device == nil {
			result.Device, result.ReportingCredential, err = issueExternalUsageDeviceCredentialTx(tx, IssueExternalUsageDeviceParams{
				UserID: token.UserID, DeviceName: params.DeviceName, DeviceFingerprint: params.DeviceFingerprint,
				AllowedSources: []string{model.ExternalUsageSourceClaudeCode},
			})
			if err != nil {
				return err
			}
		}
		result.UpstreamURL = token.UpstreamURL
		result.UpstreamIsGateway = token.UpstreamIsGateway
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
