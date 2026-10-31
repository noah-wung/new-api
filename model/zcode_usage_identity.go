package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ZCodeDuplicateIdentity struct {
	UserID        int
	EventIdentity string
	Count         int64
}

// AuditZCodeDuplicateIdentities is read-only. Historical duplicate facts and
// aggregates must be reviewed, not silently deleted or reconstructed.
func AuditZCodeDuplicateIdentities(tx *gorm.DB) ([]ZCodeDuplicateIdentity, error) {
	var duplicates []ZCodeDuplicateIdentity
	err := tx.Model(&ExternalUsageClientEvent{}).Select("user_id, event_identity, count(*) AS count").Where("source = ?", ExternalUsageSourceZCode).Group("user_id, event_identity").Having("count(*) > 1").Find(&duplicates).Error
	return duplicates, err
}

// BackfillZCodeIdentitiesTx is an explicit migration primitive. Call only in a
// transaction while ZCode reporting and retention are stopped. It preserves
// all facts and aggregates and refuses conflicting or duplicate history.
func BackfillZCodeIdentitiesTx(tx *gorm.DB) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	duplicates, err := AuditZCodeDuplicateIdentities(tx)
	if err != nil {
		return err
	}
	if len(duplicates) != 0 {
		return errors.New("duplicate historical ZCode identities require audit")
	}
	var events []ExternalUsageClientEvent
	if err := tx.Where("source = ?", ExternalUsageSourceZCode).Order("user_id, id").Find(&events).Error; err != nil {
		return err
	}
	lastUser := 0
	for i := range events {
		event := &events[i]
		if event.UserID != lastUser {
			if err := LockZCodeUsageUser(tx, event.UserID); err != nil {
				return err
			}
			lastUser = event.UserID
		}
		if strings.TrimSpace(event.EventIdentity) == "" {
			return errors.New("empty historical ZCode identity requires audit")
		}
		fingerprint, err := ZCodeUsageFingerprint(event)
		if err != nil {
			return err
		}
		var identity ZCodeUsageIdentity
		err = tx.Where("user_id = ? AND event_identity = ?", event.UserID, event.EventIdentity).First(&identity).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&ZCodeUsageIdentity{UserID: event.UserID, EventIdentity: event.EventIdentity, Fingerprint: fingerprint}).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if identity.Fingerprint != fingerprint {
			return errors.New("conflicting historical ZCode identity requires audit")
		}
	}
	return nil
}

func ZCodeUsageFingerprint(e *ExternalUsageClientEvent) (string, error) {
	value := struct {
		OccurredAt                             int64
		Session, Model                         string
		Input, Cache, Output, Reasoning, Total int
	}{e.OccurredAt, strings.TrimSpace(e.SourceSessionHash), strings.TrimSpace(e.SourceModelName), e.InputToken, e.CachedInputToken, e.OutputToken, e.ReasoningOutputToken, e.TotalToken}
	data, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ZCodeUsageLock serializes reports and scoped deletes for the same user. The
// update is a database row lock on MySQL/PostgreSQL and a write lock on SQLite.
type ZCodeUsageLock struct {
	UserID int   `gorm:"primaryKey;autoIncrement:false"`
	Serial int64 `gorm:"not null;default:0"`
}

// ZCodeUsageIdentity survives detail deletion and retention. The digest contains
// only normalized metering fields and detects conflicting replays.
type ZCodeUsageIdentity struct {
	UserID        int    `gorm:"primaryKey;autoIncrement:false"`
	EventIdentity string `gorm:"primaryKey;type:varchar(128)"`
	Fingerprint   string `gorm:"type:char(64);not null"`
}

func LockZCodeUsageUser(tx *gorm.DB, userID int) error {
	if tx == nil || userID <= 0 {
		return errors.New("invalid ZCode usage lock")
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ZCodeUsageLock{UserID: userID}).Error; err != nil {
		return err
	}
	return tx.Model(&ZCodeUsageLock{}).Where("user_id = ?", userID).UpdateColumn("serial", gorm.Expr("serial + 1")).Error
}
