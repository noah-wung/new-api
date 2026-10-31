package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_usage_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupReporterUpdateTables(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.ReporterDeployment{}, &model.ReporterRelease{}, &model.ReporterDeviceStatus{}))
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM reporter_deployments")
		model.DB.Exec("DELETE FROM reporter_releases")
		model.DB.Exec("DELETE FROM reporter_device_statuses")
	})
}

type signedReleaseFixture struct {
	root    string
	key     ed25519.PrivateKey
	catalog reporterupdate.Catalog
}

func createSignedReleaseFixture(t *testing.T) *signedReleaseFixture {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, e)
	encoded, e := common.Marshal(reporterupdate.TrustKeys{"release-key": base64.StdEncoding.EncodeToString(pub)})
	require.NoError(t, e)
	t.Setenv("NEW_API_USAGE_REPORTER_TRUST_KEYS", string(encoded))
	f := &signedReleaseFixture{root: t.TempDir(), key: key, catalog: reporterupdate.Catalog{SchemaVersion: 1, Sequence: 1, IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(), ReleaseID: "test-release-1", Version: "1.0.0", Commit: "commit-1", Protocol: reporterupdate.Range{Min: 1, Max: 1}, Config: reporterupdate.Range{Min: 1, Max: 1}, State: reporterupdate.Range{Min: 2, Max: 2}, Queue: reporterupdate.Range{Min: 1, Max: 1}, RolloutPercent: 100, RolloutSeed: "stable"}}
	t.Setenv("NEW_API_USAGE_REPORTER_RELEASE_DIR", f.root)
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
		name, e := reporterupdate.TargetName(target)
		require.NoError(t, e)
		data := []byte("signed fixture " + target)
		dir := filepath.Join(f.root, f.catalog.ReleaseID, target)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o700))
		f.catalog.Targets = append(f.catalog.Targets, reporterupdate.Artifact{Target: target, Path: "/api/external-usage/reporter/releases/" + f.catalog.ReleaseID + "/" + target + "/" + name, SHA256: reporterupdate.Digest(data), Size: int64(len(data))})
	}
	f.sign(t)
	return f
}
func (f *signedReleaseFixture) sign(t *testing.T) {
	t.Helper()
	data, e := reporterupdate.Sign(f.catalog, "release-key", f.key)
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(filepath.Join(f.root, f.catalog.ReleaseID, "catalog.json"), data, 0o600))
}

func TestReporterReleaseActivationImmutabilityPauseAndAssetValidation(t *testing.T) {
	setupReporterUpdateTables(t)
	f := createSignedReleaseFixture(t)
	require.NoError(t, ActivateReporterRelease(f.catalog.ReleaseID, 17))
	data, c, e := ActiveReporterCatalog()
	require.NoError(t, e)
	assert.NotEmpty(t, data)
	assert.Equal(t, "1.0.0", c.Version)
	require.NoError(t, model.PauseReporterDeployment())
	_, _, e = ActiveReporterCatalog()
	assert.Error(t, e)
	f.catalog.Sequence = 2
	f.catalog.RolloutPercent = 50
	f.sign(t)
	require.NoError(t, ActivateReporterRelease(f.catalog.ReleaseID, 17))
	_, c, e = ActiveReporterCatalog()
	require.NoError(t, e)
	assert.Equal(t, 50, c.RolloutPercent)
	f.catalog.Sequence = 1
	f.sign(t)
	assert.Error(t, ActivateReporterRelease(f.catalog.ReleaseID, 17), "older catalogs cannot be reactivated")
	f.catalog.Sequence = 3
	f.catalog.Commit = "different-build"
	f.sign(t)
	assert.Error(t, ActivateReporterRelease(f.catalog.ReleaseID, 17), "a version cannot be silently overwritten")
	require.NoError(t, os.WriteFile(filepath.Join(f.root, f.catalog.ReleaseID, "darwin-arm64", "usage-reporter"), []byte("tampered"), 0o700))
	assert.Error(t, ActivateReporterRelease(f.catalog.ReleaseID, 17))
}
func TestManagedInstallerRequiresSignedReleaseAndUsesOnlySelectedTokens(t *testing.T) {
	setupReporterUpdateTables(t)
	f := createSignedReleaseFixture(t)
	require.NoError(t, ActivateReporterRelease(f.catalog.ReleaseID, 17))
	for _, platform := range []string{"macos", "windows"} {
		script, e := BuildClientInstallScript(BuildInstallScriptParams{Managed: true, AutoUpdate: false, Platform: platform, ServiceURL: "https://gateway.example"}, ClientInstallTokens{Codex: "codex-token"})
		require.NoError(t, e)
		assert.Contains(t, script, "--install-clients")
		assert.Contains(t, script, "--install-auto-update=false")
		assert.Contains(t, script, "codex-token")
		assert.NotContains(t, script, "--install-claude")
		assert.Contains(t, script, "test-release-1")
		assert.NotContains(t, script, "/reporter/binary/")
	}
	_, e := BuildClientInstallScript(BuildInstallScriptParams{Managed: true, AutoUpdate: true, Platform: "macos", ServiceURL: "http://gateway.example"}, ClientInstallTokens{Codex: "token"})
	assert.Error(t, e)
}

func TestManagedZCodeInstallerChecksCapabilityBeforeExchange(t *testing.T) {
	setupReporterUpdateTables(t)
	f := createSignedReleaseFixture(t)
	require.NoError(t, ActivateReporterRelease(f.catalog.ReleaseID, 17))
	for _, platform := range []string{"macos", "windows"} {
		script, err := BuildClientInstallScript(BuildInstallScriptParams{Managed: true, Platform: platform, ServiceURL: "https://gateway.example"}, ClientInstallTokens{ZCode: "zcode-token"})
		require.NoError(t, err)
		require.Contains(t, script, "--supports-source zcode")
		require.Contains(t, script, "--zcode-install-token")
		require.Less(t, strings.Index(script, "--supports-source zcode"), strings.Index(script, "--install-clients"))
	}
	_, err := BuildClientInstallScript(BuildInstallScriptParams{Platform: "macos", ServiceURL: "https://gateway.example"}, ClientInstallTokens{ZCode: "zcode-token"})
	require.Error(t, err)
}
func TestCodexReinstallPreservesCredentialAndRejectsAccountChanges(t *testing.T) {
	setupExternalUsageTestTables(t)
	_, raw, e := IssueCodexInstallToken(17)
	require.NoError(t, e)
	first, e := ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{InstallToken: raw, DeviceFingerprint: "same-device"})
	require.NoError(t, e)
	_, raw, e = IssueCodexInstallToken(17)
	require.NoError(t, e)
	second, e := ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{InstallToken: raw, DeviceFingerprint: "same-device", ReportingCredential: first.ReportingCredential})
	require.NoError(t, e)
	assert.Equal(t, first.ReportingCredential, second.ReportingCredential)
	assert.Equal(t, first.Device.Id, second.Device.Id)
	_, raw, e = IssueCodexInstallToken(18)
	require.NoError(t, e)
	_, e = ConsumeCodexInstallToken(ConsumeCodexInstallTokenParams{InstallToken: raw, DeviceFingerprint: "same-device", ReportingCredential: first.ReportingCredential})
	assert.Error(t, e)
	_, e = ValidateCodexInstallToken(raw)
	assert.NoError(t, e)
}
func TestReporterStatusIsDeviceScopedAndRejectsRevokedOrDisabledSources(t *testing.T) {
	setupExternalUsageTestTables(t)
	setupReporterUpdateTables(t)
	settings, ok := config.GlobalConfig.Get("external_usage_setting").(*external_usage_setting.ExternalUsageSetting)
	require.True(t, ok)
	previous := *settings
	t.Cleanup(func() { *settings = previous })
	settings.Enabled = true
	settings.AllowedSources = []string{"codex"}
	require.NoError(t, model.DB.AutoMigrate(&model.User{}))
	u := model.User{Username: "reporter-status-test", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&u).Error)
	t.Cleanup(func() { model.DB.Unscoped().Delete(&u) })
	device, _, e := IssueExternalUsageDeviceCredential(IssueExternalUsageDeviceParams{UserID: u.Id, DeviceFingerprint: "status-device", AllowedSources: []string{"codex"}})
	require.NoError(t, e)
	status := model.ReporterDeviceStatus{DeviceID: 9999, UserID: 9999, Source: "codex", Version: "1.0.0", AutoUpdate: true, Phase: "idle", LastCheck: time.Now().Unix()}
	require.NoError(t, SaveReporterDeviceStatus(device, status))
	rows, e := model.ListReporterStatuses(u.Id)
	require.NoError(t, e)
	require.Len(t, rows, 1)
	assert.Equal(t, device.Id, rows[0].DeviceID)
	other, e := model.ListReporterStatuses(9999)
	require.NoError(t, e)
	assert.Empty(t, other)
	status.Source = "claude_code"
	assert.Error(t, SaveReporterDeviceStatus(device, status))
	status.Source = "codex"
	status.ErrorCode = "secret file path"
	assert.Error(t, SaveReporterDeviceStatus(device, status))
	status.ErrorCode = ""
	device.Status = model.ExternalUsageStatusRevoked
	assert.Error(t, SaveReporterDeviceStatus(device, status))
	device.Status = model.ExternalUsageStatusActive
	settings.AllowedSources = []string{"claude_code"}
	assert.Error(t, SaveReporterDeviceStatus(device, status))
}
