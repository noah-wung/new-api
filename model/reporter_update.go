package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ReporterDeployment struct {
	ID          int    `json:"id" gorm:"primaryKey;autoIncrement:false"`
	Sequence    int64  `json:"sequence"`
	ReleaseID   string `json:"release_id" gorm:"type:varchar(96)"`
	Catalog     string `json:"-" gorm:"type:text"`
	CatalogHash string `json:"catalog_hash" gorm:"type:varchar(64)"`
	Paused      bool   `json:"paused"`
	UpdatedAt   int64  `json:"updated_at"`
}
type ReporterRelease struct {
	ID             int    `json:"id"`
	ReleaseID      string `json:"release_id" gorm:"type:varchar(96);uniqueIndex"`
	Version        string `json:"reporter_version" gorm:"type:varchar(64);uniqueIndex"`
	ArtifactDigest string `json:"artifact_digest" gorm:"type:varchar(64)"`
	ActivatedBy    int    `json:"activated_by"`
	CreatedAt      int64  `json:"created_at"`
}
type ReporterDeviceStatus struct {
	DeviceID   int    `json:"device_id" gorm:"primaryKey;autoIncrement:false"`
	UserID     int    `json:"-" gorm:"index"`
	Source     string `json:"source" gorm:"type:varchar(32)"`
	Version    string `json:"reporter_version" gorm:"type:varchar(64)"`
	Available  string `json:"available_version" gorm:"type:varchar(64)"`
	AutoUpdate bool   `json:"auto_update"`
	Phase      string `json:"phase" gorm:"type:varchar(32)"`
	ErrorCode  string `json:"error_code" gorm:"type:varchar(48)"`
	LastCheck  int64  `json:"last_check_at"`
	ReceivedAt int64  `json:"received_at"`
}

func ActivateReporterDeployment(release ReporterRelease, deployment ReporterDeployment) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ReporterDeployment{ID: 1}).Error; e != nil {
			return e
		}
		var old ReporterDeployment
		if e := lockForUpdate(tx).First(&old, 1).Error; e != nil {
			return e
		}
		if deployment.Sequence < old.Sequence || (deployment.Sequence == old.Sequence && deployment.CatalogHash != old.CatalogHash) {
			return errors.New("catalog sequence must increase")
		}
		var previous ReporterRelease
		e := tx.Where("release_id = ? OR version = ?", release.ReleaseID, release.Version).First(&previous).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e == nil {
			if previous.ReleaseID != release.ReleaseID || previous.Version != release.Version || previous.ArtifactDigest != release.ArtifactDigest {
				return errors.New("published release is immutable")
			}
		} else {
			release.CreatedAt = common.GetTimestamp()
			if e = tx.Create(&release).Error; e != nil {
				return e
			}
		}
		deployment.ID = 1
		deployment.UpdatedAt = common.GetTimestamp()
		return tx.Save(&deployment).Error
	})
}
func GetReporterDeployment() (*ReporterDeployment, error) {
	var d ReporterDeployment
	e := DB.First(&d, 1).Error
	return &d, e
}
func PauseReporterDeployment() error {
	return DB.Model(&ReporterDeployment{}).Where("id = ?", 1).Updates(map[string]any{"paused": true, "updated_at": common.GetTimestamp()}).Error
}
func ListReporterReleases() ([]ReporterRelease, error) {
	var rows []ReporterRelease
	e := DB.Order("id DESC").Limit(100).Find(&rows).Error
	return rows, e
}
func ListReporterStatuses(userID int) ([]ReporterDeviceStatus, error) {
	var rows []ReporterDeviceStatus
	e := DB.Where("user_id = ?", userID).Find(&rows).Error
	return rows, e
}
func SaveReporterStatus(status ReporterDeviceStatus) error {
	return DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}}, DoUpdates: clause.AssignmentColumns([]string{"user_id", "source", "version", "available", "auto_update", "phase", "error_code", "last_check", "received_at"})}).Create(&status).Error
}
