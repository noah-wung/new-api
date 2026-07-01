package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ExternalUsageInstallToken struct {
	Id          int    `json:"id"`
	UserID      int    `json:"user_id" gorm:"index"`
	Source      string `json:"source" gorm:"type:varchar(32);default:'';index"`
	TokenPrefix string `json:"token_prefix" gorm:"type:varchar(24);default:'';index"`
	TokenHash   string `json:"-" gorm:"type:varchar(128);uniqueIndex"`
	Status      string `json:"status" gorm:"type:varchar(32);default:'active';index"`
	ExpiresAt   int64  `json:"expires_at" gorm:"bigint;index"`
	ConsumedAt  int64  `json:"consumed_at" gorm:"bigint"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
}

func (t *ExternalUsageInstallToken) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = ExternalUsageStatusActive
	}
	t.Source = strings.TrimSpace(t.Source)
	return nil
}

func (t *ExternalUsageInstallToken) BeforeUpdate(tx *gorm.DB) error {
	t.UpdatedAt = common.GetTimestamp()
	t.Source = strings.TrimSpace(t.Source)
	return nil
}

func CreateExternalUsageInstallToken(tx *gorm.DB, token *ExternalUsageInstallToken) error {
	if tx == nil {
		tx = DB
	}
	return tx.Create(token).Error
}

func UpdateExternalUsageInstallToken(tx *gorm.DB, token *ExternalUsageInstallToken) error {
	if tx == nil {
		tx = DB
	}
	return tx.Save(token).Error
}

func GetExternalUsageInstallTokenByHash(tx *gorm.DB, tokenHash string) (*ExternalUsageInstallToken, error) {
	if tx == nil {
		tx = DB
	}
	var token ExternalUsageInstallToken
	if err := tx.Where("token_hash = ?", strings.TrimSpace(tokenHash)).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}
