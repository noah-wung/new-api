package external_usage_setting

import "testing"

func TestDefaultSetting(t *testing.T) {
	setting := GetSetting()
	if setting.MaxDevicesPerUser != 3 {
		t.Fatalf("MaxDevicesPerUser = %d, want 3", setting.MaxDevicesPerUser)
	}
	if setting.DetailRetentionDays != 180 {
		t.Fatalf("DetailRetentionDays = %d, want 180", setting.DetailRetentionDays)
	}
	if setting.AcceptWindowDays != 90 {
		t.Fatalf("AcceptWindowDays = %d, want 90", setting.AcceptWindowDays)
	}
	if len(setting.AllowedSources) != 1 || setting.AllowedSources[0] != "codex" {
		t.Fatalf("AllowedSources = %v, want [codex]", setting.AllowedSources)
	}
}

func TestGetSettingNormalizesLimitsAndSources(t *testing.T) {
	previous := externalUsageSetting
	externalUsageSetting = ExternalUsageSetting{
		Enabled:             true,
		MaxDevicesPerUser:   99,
		DetailRetentionDays: -1,
		AcceptWindowDays:    -5,
		AllowedSources:      []string{" Codex ", "", "ZCode", "codex"},
	}
	t.Cleanup(func() {
		externalUsageSetting = previous
	})

	setting := GetSetting()
	if setting.MaxDevicesPerUser != 3 {
		t.Fatalf("MaxDevicesPerUser = %d, want 3", setting.MaxDevicesPerUser)
	}
	if setting.DetailRetentionDays != 0 {
		t.Fatalf("DetailRetentionDays = %d, want 0", setting.DetailRetentionDays)
	}
	if setting.AcceptWindowDays != 0 {
		t.Fatalf("AcceptWindowDays = %d, want 0", setting.AcceptWindowDays)
	}
	if len(setting.AllowedSources) != 2 || setting.AllowedSources[0] != "codex" || setting.AllowedSources[1] != "zcode" {
		t.Fatalf("AllowedSources = %v, want [codex zcode]", setting.AllowedSources)
	}
}
