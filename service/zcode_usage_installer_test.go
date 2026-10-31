package service

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestZCodeInstallTokenIsSingleUseAndSourceBound(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "usage.sqlite")), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	setupExternalUsageTestTables(t)
	cfg := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.Enabled = true
	cfg.MaxDevicesPerUser = 1
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex, model.ExternalUsageSourceClaudeCode, model.ExternalUsageSourceZCode}
	_, codex, err := IssueCodexInstallToken(900)
	require.NoError(t, err)
	_, err = ConsumeZCodeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: codex, DeviceFingerprint: "zcode-device"})
	require.Error(t, err)
	_, raw, err := IssueZCodeInstallToken(900)
	require.NoError(t, err)
	_, err = ConsumeClaudeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: raw, DeviceFingerprint: "zcode-device"})
	require.Error(t, err)
	result, err := ConsumeZCodeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: raw, DeviceName: "fixture", DeviceFingerprint: "zcode-device"})
	require.NoError(t, err)
	require.Equal(t, []string{model.ExternalUsageSourceZCode}, result.Device.GetAllowedSources())
	_, err = ConsumeZCodeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: raw, DeviceFingerprint: "zcode-device"})
	require.Error(t, err)
	_, second, err := IssueZCodeInstallToken(900)
	require.NoError(t, err)
	_, err = ConsumeZCodeInstallToken(ConsumeZCodeInstallTokenParams{InstallToken: second, DeviceFingerprint: "different-device"})
	require.Error(t, err)
	reinstalled, err := ConsumeZCodeInstallToken(ConsumeZCodeInstallTokenParams{InstallToken: second, DeviceFingerprint: "zcode-device", ReportingCredential: result.ReportingCredential})
	require.NoError(t, err)
	require.Equal(t, result.ReportingCredential, reinstalled.ReportingCredential)
	_, otherUser, err := IssueZCodeInstallToken(901)
	require.NoError(t, err)
	require.Error(t, ValidateClientInstallTokens(ClientInstallTokens{Codex: codex, ZCode: otherUser}))
	_, err = ConsumeZCodeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: otherUser, DeviceFingerprint: "zcode-device", ReportingCredential: result.ReportingCredential})
	require.Error(t, err)
	_, expiredRaw, err := IssueZCodeInstallToken(900)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.ExternalUsageInstallToken{}).Where("token_hash = ?", sha256Hex(expiredRaw)).Update("expires_at", 1).Error)
	_, err = ConsumeZCodeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: expiredRaw, DeviceFingerprint: "zcode-device"})
	require.Error(t, err)
}
