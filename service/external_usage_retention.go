package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const externalUsageRetentionTickInterval = 24 * time.Hour

type ExternalUsageRetentionResult struct {
	CursorEventsDeleted int64 `json:"cursor_events_deleted"`
	ClientEventsDeleted int64 `json:"client_events_deleted"`
	CutoffOccurredAt    int64 `json:"cutoff_occurred_at"`
}

var (
	externalUsageRetentionOnce    sync.Once
	externalUsageRetentionRunning atomic.Bool
)

func StartExternalUsageRetentionTask() {
	externalUsageRetentionOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("external usage retention task started: tick=%s", externalUsageRetentionTickInterval))
			ticker := time.NewTicker(externalUsageRetentionTickInterval)
			defer ticker.Stop()

			runExternalUsageRetentionOnce()
			for range ticker.C {
				runExternalUsageRetentionOnce()
			}
		})
	})
}

func runExternalUsageRetentionOnce() {
	if !externalUsageRetentionRunning.CompareAndSwap(false, true) {
		return
	}
	defer externalUsageRetentionRunning.Store(false)

	result, err := CleanupExpiredExternalUsageDetails(time.Now().Unix())
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("external usage retention failed: %v", err))
		return
	}
	if common.DebugEnabled && (result.CursorEventsDeleted > 0 || result.ClientEventsDeleted > 0) {
		logger.LogDebug(context.Background(),
			"external usage retention: cursor_deleted=%d client_deleted=%d cutoff=%d",
			result.CursorEventsDeleted,
			result.ClientEventsDeleted,
			result.CutoffOccurredAt,
		)
	}
}

func CleanupExpiredExternalUsageDetails(now int64) (*ExternalUsageRetentionResult, error) {
	setting := external_usage_setting.GetSetting()
	if setting.DetailRetentionDays <= 0 {
		return &ExternalUsageRetentionResult{}, nil
	}
	cutoff := now - int64(setting.DetailRetentionDays)*24*3600
	result := &ExternalUsageRetentionResult{CutoffOccurredAt: cutoff}

	err := model.DB.Transaction(func(tx *gorm.DB) error {
		cursorDelete := tx.Where("occurred_at < ?", cutoff).Delete(&model.ExternalUsageCursorEvent{})
		if cursorDelete.Error != nil {
			return cursorDelete.Error
		}
		result.CursorEventsDeleted = cursorDelete.RowsAffected

		clientDelete := tx.Where("occurred_at < ?", cutoff).Delete(&model.ExternalUsageClientEvent{})
		if clientDelete.Error != nil {
			return clientDelete.Error
		}
		result.ClientEventsDeleted = clientDelete.RowsAffected
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
