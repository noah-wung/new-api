package external_usage_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type ExternalUsageSetting struct {
	Enabled             bool     `json:"enabled"`
	MaxDevicesPerUser   int      `json:"max_devices_per_user"`
	DetailRetentionDays int      `json:"detail_retention_days"`
	AcceptWindowDays    int      `json:"accept_window_days"`
	AllowedSources      []string `json:"allowed_sources"`
}

var externalUsageSetting = ExternalUsageSetting{
	Enabled:             false,
	MaxDevicesPerUser:   3,
	DetailRetentionDays: 180,
	AcceptWindowDays:    90,
	AllowedSources:      []string{"codex"},
}

func init() {
	config.GlobalConfig.Register("external_usage_setting", &externalUsageSetting)
}

func GetSetting() ExternalUsageSetting {
	setting := externalUsageSetting
	if setting.MaxDevicesPerUser <= 0 {
		setting.MaxDevicesPerUser = 1
	}
	if setting.MaxDevicesPerUser > 3 {
		setting.MaxDevicesPerUser = 3
	}
	if setting.DetailRetentionDays < 0 {
		setting.DetailRetentionDays = 0
	}
	if setting.AcceptWindowDays < 0 {
		setting.AcceptWindowDays = 0
	}
	setting.AllowedSources = normalizeAllowedSources(setting.AllowedSources)
	if len(setting.AllowedSources) == 0 {
		setting.AllowedSources = append(setting.AllowedSources, "codex")
	}
	return setting
}

func IsEnabled() bool {
	return externalUsageSetting.Enabled
}

func normalizeAllowedSources(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		value := strings.ToLower(strings.TrimSpace(item))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}
