package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildInstallScript_ReinstallPathDoesNotDeleteState(t *testing.T) {
	script, err := BuildInstallScript(BuildInstallScriptParams{
		Platform:         ExternalUsageInstallerPlatformMacOS,
		ServiceURL:       "https://example.com",
		InstallToken:     "install-token-1",
		AcceptWindowDays: 90,
	})
	if err != nil {
		t.Fatalf("BuildInstallScript returned error: %v", err)
	}

	if !strings.Contains(script, "state.json") {
		t.Fatal("expected reinstall script to reference state.json")
	}
	if strings.Contains(script, "rm -f \"$STATE_PATH\"") || strings.Contains(script, "Remove-Item $StatePath") {
		t.Fatal("reinstall script should preserve state.json instead of deleting it")
	}
}

func TestBuildInstallScript_SelectsMacOSArchBinary(t *testing.T) {
	script, err := BuildInstallScript(BuildInstallScriptParams{
		Platform:         ExternalUsageInstallerPlatformMacOS,
		ServiceURL:       "https://example.com",
		InstallToken:     "install-token-2",
		AcceptWindowDays: 90,
	})
	if err != nil {
		t.Fatalf("BuildInstallScript returned error: %v", err)
	}

	if !strings.Contains(script, "uname -m") {
		t.Fatal("expected macOS install script to detect uname -m")
	}
	if !strings.Contains(script, "darwin-arm64") {
		t.Fatal("expected macOS install script to select darwin-arm64")
	}
	if !strings.Contains(script, "darwin-amd64") {
		t.Fatal("expected macOS install script to select darwin-amd64")
	}
}

func TestBuildInstallScript_MacOSIncludesRealInstallFlow(t *testing.T) {
	script, err := BuildInstallScript(BuildInstallScriptParams{
		Platform:         ExternalUsageInstallerPlatformMacOS,
		ServiceURL:       "https://example.com",
		InstallToken:     "install-token-4",
		AcceptWindowDays: 90,
	})
	if err != nil {
		t.Fatalf("BuildInstallScript returned error: %v", err)
	}

	checks := []string{
		"/api/external-usage/reporter/binary/$TARGET/usage-reporter",
		"/api/external-usage/self/codex-installer/exchange",
		"curl -fsSL \"$BIN_URL\" -o \"$BIN_PATH\"",
		"\"$BIN_PATH\" --config \"$CONFIG_PATH\"",
		"LaunchAgents/pro.newapi.usage-reporter.codex.plist",
		"launchctl",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Fatalf("macOS install script missing %q", check)
		}
	}
	if !strings.Contains(script, `"accept_window_days":$ACCEPT_WINDOW_DAYS`) {
		t.Fatal("expected macOS install script to persist accept_window_days from the install script variable")
	}
}

func TestBuildInstallScript_MacOSDoesNotDoubleTriggerReporterAtInstall(t *testing.T) {
	script, err := BuildInstallScript(BuildInstallScriptParams{
		Platform:         ExternalUsageInstallerPlatformMacOS,
		ServiceURL:       "https://example.com",
		InstallToken:     "install-token-6",
		AcceptWindowDays: 90,
	})
	if err != nil {
		t.Fatalf("BuildInstallScript returned error: %v", err)
	}

	if !strings.Contains(script, `"$BIN_PATH" --config "$CONFIG_PATH"`) {
		t.Fatal("expected macOS install script to keep the initial foreground report run")
	}
	if strings.Contains(script, "<key>RunAtLoad</key>") {
		t.Fatal("macOS install script should not also run the LaunchAgent immediately after the initial foreground report")
	}
}

func TestBuildInstallScript_WindowsIncludesRealInstallFlow(t *testing.T) {
	script, err := BuildInstallScript(BuildInstallScriptParams{
		Platform:         ExternalUsageInstallerPlatformWindows,
		ServiceURL:       "https://example.com",
		InstallToken:     "install-token-5",
		AcceptWindowDays: 90,
	})
	if err != nil {
		t.Fatalf("BuildInstallScript returned error: %v", err)
	}

	checks := []string{
		"/api/external-usage/reporter/binary/windows-amd64/usage-reporter.exe",
		"/api/external-usage/self/codex-installer/exchange",
		"Invoke-RestMethod",
		"usage-reporter.exe",
		"--config",
		"schtasks",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Fatalf("Windows install script missing %q", check)
		}
	}
	if strings.Contains(script, "Set-Content -Path $ConfigPath -Value $Config -Encoding UTF8") {
		t.Fatal("Windows install script should avoid PowerShell UTF-8 BOM config writes")
	}
	if !strings.Contains(script, "accept_window_days = $AcceptWindowDays") {
		t.Fatal("expected Windows install script to persist accept_window_days from the install script variable")
	}
}

func TestBuildInstallScript_WindowsSchedulesSilentBackgroundRunner(t *testing.T) {
	script, err := BuildInstallScript(BuildInstallScriptParams{
		Platform:         ExternalUsageInstallerPlatformWindows,
		ServiceURL:       "https://example.com",
		InstallToken:     "install-token-7",
		AcceptWindowDays: 90,
	})
	if err != nil {
		t.Fatalf("BuildInstallScript returned error: %v", err)
	}

	if !strings.Contains(script, "-WindowStyle Hidden") {
		t.Fatal("expected Windows install script to schedule reporter with a hidden PowerShell window")
	}
	if !strings.Contains(script, `Join-Path $InstallRoot "run.ps1"`) {
		t.Fatal("expected Windows install script to write a dedicated silent runner script")
	}
	if !strings.Contains(script, `*>> "$LogPath"`) {
		t.Fatal("expected Windows install script to redirect scheduled reporter output to a log file")
	}
}

func TestResolveUsageReporterBinaryPath_UsesConfiguredAssetDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "darwin-arm64", "usage-reporter")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(path, []byte("binary-data"), 0o755); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	t.Setenv("NEW_API_USAGE_REPORTER_ASSET_DIR", dir)

	spec, err := ResolveUsageReporterBinarySpec("darwin-arm64")
	if err != nil {
		t.Fatalf("ResolveUsageReporterBinarySpec returned error: %v", err)
	}
	if spec.Path != path {
		t.Fatalf("spec.Path = %q, want %q", spec.Path, path)
	}
	if spec.DownloadName != "usage-reporter" {
		t.Fatalf("spec.DownloadName = %q, want usage-reporter", spec.DownloadName)
	}
}

func TestBuildInstallerCommand_RendersPlatformSpecificInvoker(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		want     string
	}{
		{
			name:     "macOS",
			platform: ExternalUsageInstallerPlatformMacOS,
			want:     "install.sh?token=install-token-3",
		},
		{
			name:     "Windows",
			platform: ExternalUsageInstallerPlatformWindows,
			want:     "install.ps1?token=install-token-3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			command, err := BuildInstallerCommand(BuildInstallScriptParams{
				Platform:     tc.platform,
				ServiceURL:   "https://example.com",
				InstallToken: "install-token-3",
			})
			if err != nil {
				t.Fatalf("BuildInstallerCommand returned error: %v", err)
			}
			if !strings.Contains(command, tc.want) {
				t.Fatalf("command = %q, want substring %q", command, tc.want)
			}
		})
	}
}
