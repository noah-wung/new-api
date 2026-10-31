package service

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type ConsumeZCodeInstallTokenParams = ConsumeClaudeInstallTokenParams
type ConsumeZCodeInstallTokenResult = ConsumeClaudeInstallTokenResult

func IssueZCodeInstallToken(userID int) (*model.ExternalUsageInstallToken, string, error) {
	if err := ValidateExternalUsageSourceEnabled(model.ExternalUsageSourceZCode); err != nil {
		return nil, "", err
	}
	key, err := common.GenerateKey()
	if err != nil {
		return nil, "", err
	}
	raw := "eut_" + key
	token := &model.ExternalUsageInstallToken{UserID: userID, Source: model.ExternalUsageSourceZCode, TokenPrefix: raw[:16], TokenHash: sha256Hex(raw), Status: model.ExternalUsageStatusActive, ExpiresAt: common.GetTimestamp() + int64(codexInstallTokenTTL/time.Second)}
	if err := model.CreateExternalUsageInstallToken(nil, token); err != nil {
		return nil, "", err
	}
	return token, raw, nil
}

func ConsumeZCodeInstallToken(params ConsumeZCodeInstallTokenParams) (*ConsumeZCodeInstallTokenResult, error) {
	result := &ConsumeZCodeInstallTokenResult{}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		token, err := validateActiveUsageInstallTokenTx(tx, params.InstallToken, model.ExternalUsageSourceZCode)
		if err != nil {
			return err
		}
		claim := tx.Model(&model.ExternalUsageInstallToken{}).Where("id = ? AND status = ? AND consumed_at = ?", token.Id, model.ExternalUsageStatusActive, 0).Updates(map[string]any{"status": model.ExternalUsageStatusConsumed, "consumed_at": common.GetTimestamp(), "updated_at": common.GetTimestamp()})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return model.ErrExternalUsageInstallTokenConsumed
		}
		previousCredential := strings.TrimSpace(params.ReportingCredential)
		if previousCredential != "" {
			var previous model.ExternalUsageDevice
			err := tx.Where("credential_hash = ? AND user_id = ? AND device_fingerprint_hash = ? AND status = ?", sha256Hex(previousCredential), token.UserID, sha256Hex(strings.TrimSpace(params.DeviceFingerprint)), model.ExternalUsageStatusActive).First(&previous).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err != nil || !isSourceAllowed(previous.GetAllowedSources(), model.ExternalUsageSourceZCode) {
				return errors.New("existing installation identity must be resolved before reinstalling")
			}
			result.Device, result.ReportingCredential = &previous, previousCredential
		}
		if result.Device == nil {
			result.Device, result.ReportingCredential, err = issueExternalUsageDeviceCredentialTx(tx, IssueExternalUsageDeviceParams{UserID: token.UserID, DeviceName: params.DeviceName, DeviceFingerprint: params.DeviceFingerprint, AllowedSources: []string{model.ExternalUsageSourceZCode}})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
