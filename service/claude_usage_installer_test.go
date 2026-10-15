package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeInstallTokenSourceBindingReuseAndRollback(t *testing.T) {
	setupExternalUsageTestTables(t)
	cfg, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.AllowedSources = []string{model.ExternalUsageSourceCodex, model.ExternalUsageSourceClaudeCode}
	cfg.MaxDevicesPerUser = 1
	_, bad, err := IssueClaudeInstallToken(17, "https://secret@api.anthropic.com", false)
	require.Error(t, err)
	assert.Empty(t, bad)
	_, codexRaw, err := IssueCodexInstallToken(17)
	require.NoError(t, err)
	_, err = ConsumeClaudeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: codexRaw, DeviceFingerprint: "device"})
	require.Error(t, err)
	_, err = ValidateCodexInstallToken(codexRaw)
	require.NoError(t, err, "wrong-source exchange must not consume the token")
	token, raw, err := IssueClaudeInstallToken(17, "https://API.anthropic.com:443/v1", false)
	require.NoError(t, err)
	assert.Equal(t, "https://api.anthropic.com", token.UpstreamURL)
	_, err = ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{InstallToken: raw, DeviceFingerprint: "device"})
	require.Error(t, err)
	first, err := ConsumeClaudeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: raw, DeviceFingerprint: "device"})
	require.NoError(t, err)
	assert.Equal(t, []string{model.ExternalUsageSourceClaudeCode}, first.Device.GetAllowedSources())
	_, err = ConsumeClaudeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: raw, DeviceFingerprint: "device"})
	assert.ErrorIs(t, err, model.ErrExternalUsageInstallTokenConsumed)
	_, repeat, err := IssueClaudeInstallToken(17, "https://api.anthropic.com", false)
	require.NoError(t, err)
	_, err = ConsumeClaudeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: repeat, DeviceFingerprint: "other-device"})
	assert.ErrorIs(t, err, model.ErrExternalUsageDeviceLimitExceeded)
	_, err = ValidateClaudeInstallToken(repeat)
	require.NoError(t, err, "failed device creation must roll back consumption")
	second, err := ConsumeClaudeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: repeat, DeviceFingerprint: "device", ReportingCredential: first.ReportingCredential})
	require.NoError(t, err)
	assert.Equal(t, first.Device.Id, second.Device.Id)
	assert.Equal(t, first.ReportingCredential, second.ReportingCredential, "reinstall must retain pending queue identity")
	_, differentUser, err := IssueClaudeInstallToken(18, "https://api.anthropic.com", false)
	require.NoError(t, err)
	other, err := ConsumeClaudeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: differentUser, DeviceFingerprint: "device", ReportingCredential: first.ReportingCredential})
	require.NoError(t, err)
	assert.NotEqual(t, first.ReportingCredential, other.ReportingCredential)
	assert.Equal(t, 18, other.Device.UserID)
	expired, expiredRaw, err := IssueClaudeInstallToken(17, "https://api.anthropic.com", false)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(expired).Update("expires_at", common.GetTimestamp()-int64(time.Minute/time.Second)).Error)
	_, err = ConsumeClaudeInstallToken(ConsumeClaudeInstallTokenParams{InstallToken: expiredRaw})
	assert.ErrorIs(t, err, model.ErrExternalUsageInstallTokenExpired)
}
