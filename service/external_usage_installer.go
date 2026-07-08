package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	ExternalUsageInstallerPlatformMacOS   = "macos"
	ExternalUsageInstallerPlatformWindows = "windows"

	usageReporterAssetDirEnv  = "NEW_API_USAGE_REPORTER_ASSET_DIR"
	usageReporterBinaryPrefix = "new-api-usage-reporter-binaries"
)

type ConsumeCodexInstallTokenParams struct {
	InstallToken      string
	DeviceName        string
	DeviceFingerprint string
}

type ConsumeCodexInstallTokenResult struct {
	InstallToken        *model.ExternalUsageInstallToken `json:"install_token"`
	Device              *model.ExternalUsageDevice       `json:"device"`
	ReportingCredential string                           `json:"reporting_credential"`
	Source              string                           `json:"source"`
}

type BuildInstallScriptParams struct {
	Platform         string
	ServiceURL       string
	InstallToken     string
	AcceptWindowDays int
}

type UsageReporterBinarySpec struct {
	Target       string
	GOOS         string
	GOARCH       string
	DownloadName string
	Path         string
}

func IssueCodexInstallToken(userID int) (*model.ExternalUsageInstallToken, string, error) {
	rawToken, err := common.GenerateKey()
	if err != nil {
		return nil, "", err
	}
	rawToken = "eut_" + rawToken
	tokenPrefix := rawToken
	if len(tokenPrefix) > 16 {
		tokenPrefix = tokenPrefix[:16]
	}

	token := &model.ExternalUsageInstallToken{
		UserID:      userID,
		Source:      model.ExternalUsageSourceCodex,
		TokenPrefix: tokenPrefix,
		TokenHash:   sha256Hex(rawToken),
		Status:      model.ExternalUsageStatusActive,
		ExpiresAt:   common.GetTimestamp() + int64(codexInstallTokenTTL/time.Second),
	}
	if err := model.CreateExternalUsageInstallToken(nil, token); err != nil {
		return nil, "", err
	}
	return token, rawToken, nil
}

func ConsumeCodexInstallToken(params ConsumeCodexInstallTokenParams) (*ConsumeCodexInstallTokenResult, error) {
	result := &ConsumeCodexInstallTokenResult{Source: model.ExternalUsageSourceCodex}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		installToken, err := validateActiveCodexInstallTokenTx(tx, params.InstallToken)
		if err != nil {
			return err
		}

		device, reportingCredential, err := issueExternalUsageDeviceCredentialTx(tx, IssueExternalUsageDeviceParams{
			UserID:            installToken.UserID,
			DeviceName:        params.DeviceName,
			DeviceFingerprint: params.DeviceFingerprint,
			AllowedSources:    []string{model.ExternalUsageSourceCodex},
		})
		if err != nil {
			return err
		}

		installToken.Status = model.ExternalUsageStatusConsumed
		installToken.ConsumedAt = common.GetTimestamp()
		if err := model.UpdateExternalUsageInstallToken(tx, installToken); err != nil {
			return err
		}

		result.InstallToken = installToken
		result.Device = device
		result.ReportingCredential = reportingCredential
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ValidateCodexInstallToken(rawToken string) (*model.ExternalUsageInstallToken, error) {
	return validateActiveCodexInstallTokenTx(model.DB, rawToken)
}

func BuildInstallScript(params BuildInstallScriptParams) (string, error) {
	if err := ValidateInstallerPlatform(params.Platform); err != nil {
		return "", err
	}
	switch strings.TrimSpace(params.Platform) {
	case ExternalUsageInstallerPlatformMacOS:
		return buildMacOSInstallScript(params), nil
	case ExternalUsageInstallerPlatformWindows:
		return buildWindowsInstallScript(params), nil
	}
	return "", fmt.Errorf("unsupported installer platform: %s", params.Platform)
}

func BuildInstallerCommand(params BuildInstallScriptParams) (string, error) {
	if err := ValidateInstallerPlatform(params.Platform); err != nil {
		return "", err
	}
	serviceURL := strings.TrimRight(strings.TrimSpace(params.ServiceURL), "/")
	installToken := strings.TrimSpace(params.InstallToken)
	switch strings.TrimSpace(params.Platform) {
	case ExternalUsageInstallerPlatformMacOS:
		return fmt.Sprintf("bash -c \"$(curl -fsSL '%s/api/external-usage/reporter/install.sh?token=%s')\"", serviceURL, installToken), nil
	case ExternalUsageInstallerPlatformWindows:
		return fmt.Sprintf("powershell -ExecutionPolicy Bypass -Command \"iwr '%s/api/external-usage/reporter/install.ps1?token=%s' -UseBasicParsing | iex\"", serviceURL, installToken), nil
	}
	return "", fmt.Errorf("unsupported installer platform: %s", params.Platform)
}

func ResolveUsageReporterBinarySpec(target string) (*UsageReporterBinarySpec, error) {
	spec, err := usageReporterBinarySpecForTarget(target)
	if err != nil {
		return nil, err
	}

	for _, dir := range usageReporterBinarySearchDirs() {
		candidate := filepath.Join(dir, spec.Target, spec.DownloadName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			spec.Path = candidate
			return spec, nil
		}
	}

	builtPath, buildErr := buildUsageReporterBinary(spec)
	if buildErr != nil {
		return nil, buildErr
	}
	spec.Path = builtPath
	return spec, nil
}

func buildMacOSInstallScript(params BuildInstallScriptParams) string {
	serviceURL := strings.TrimRight(strings.TrimSpace(params.ServiceURL), "/")
	installToken := strings.TrimSpace(params.InstallToken)
	return fmt.Sprintf(`#!/bin/sh
set -eu

SERVICE_URL=%q
INSTALL_TOKEN=%q
ACCEPT_WINDOW_DAYS=%d
INSTALL_ROOT="$HOME/.new-api-usage-reporter"
BIN_PATH="$INSTALL_ROOT/usage-reporter"
CONFIG_PATH="$INSTALL_ROOT/config.json"
STATE_PATH="$INSTALL_ROOT/state.json"
CODEX_DIR="$HOME/.codex/sessions"
PLIST_PATH="$HOME/Library/LaunchAgents/pro.newapi.usage-reporter.codex.plist"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}

trap cleanup EXIT INT TERM

mkdir -p "$INSTALL_ROOT"
mkdir -p "$(dirname "$PLIST_PATH")"

ARCH="$(uname -m)"
case "$ARCH" in
  arm64|aarch64)
    TARGET="darwin-arm64"
    ;;
  x86_64|amd64)
    TARGET="darwin-amd64"
    ;;
  *)
    echo "unsupported macOS architecture: $ARCH" >&2
    exit 1
    ;;
esac

BIN_URL="$SERVICE_URL/api/external-usage/reporter/binary/$TARGET/usage-reporter"
EXCHANGE_URL="$SERVICE_URL/api/external-usage/self/codex-installer/exchange"
DEVICE_NAME="$(hostname 2>/dev/null || echo 'codex-macos')"
DEVICE_FINGERPRINT="$(printf 'codex|%%s|%%s|%%s' "$TARGET" "$DEVICE_NAME" "$HOME" | shasum -a 256 | awk '{print $1}')"
EXCHANGE_RESPONSE="$TMP_DIR/exchange.json"

curl -fsSL "$BIN_URL" -o "$BIN_PATH"
chmod +x "$BIN_PATH"

PAYLOAD=$(printf '{"install_token":"%%s","device_name":"%%s","device_fingerprint":"%%s"}' "$INSTALL_TOKEN" "$DEVICE_NAME" "$DEVICE_FINGERPRINT")
curl -fsSL -X POST "$EXCHANGE_URL" -H 'Content-Type: application/json' -d "$PAYLOAD" -o "$EXCHANGE_RESPONSE"

parse_json_field() {
  key="$1"
  sed -n 's/.*"'$1'":"\([^"]*\)".*/\1/p' "$EXCHANGE_RESPONSE" | head -n 1
}

REPORTING_CREDENTIAL="$(parse_json_field reporting_credential)"
SOURCE="$(parse_json_field source)"
SERVER="$(parse_json_field server)"

if [ -z "$REPORTING_CREDENTIAL" ]; then
  echo "installer exchange did not return a reporting credential" >&2
  cat "$EXCHANGE_RESPONSE" >&2
  exit 1
fi

if [ -z "$SOURCE" ]; then
  SOURCE="codex"
fi
if [ -z "$SERVER" ]; then
  SERVER="$SERVICE_URL"
fi

cat > "$CONFIG_PATH" <<EOF
{"server":"$SERVER","source":"$SOURCE","credential":"$REPORTING_CREDENTIAL","codex_dir":"$CODEX_DIR","state_path":"$STATE_PATH","accept_window_days":$ACCEPT_WINDOW_DAYS}
EOF

"$BIN_PATH" --config "$CONFIG_PATH"

cat > "$PLIST_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>pro.newapi.usage-reporter.codex</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN_PATH</string>
    <string>--config</string>
    <string>$CONFIG_PATH</string>
  </array>
  <key>StartInterval</key>
  <integer>300</integer>
  <key>StandardOutPath</key>
  <string>$INSTALL_ROOT/launchagent.log</string>
  <key>StandardErrorPath</key>
  <string>$INSTALL_ROOT/launchagent.err.log</string>
</dict>
</plist>
EOF

launchctl unload "$PLIST_PATH" >/dev/null 2>&1 || true
launchctl load -w "$PLIST_PATH"

echo "Installed Codex usage reporting to $INSTALL_ROOT"
`, serviceURL, installToken, params.AcceptWindowDays)
}

func buildWindowsInstallScript(params BuildInstallScriptParams) string {
	serviceURL := strings.TrimRight(strings.TrimSpace(params.ServiceURL), "/")
	installToken := strings.TrimSpace(params.InstallToken)
	return fmt.Sprintf(`$ServiceUrl = %q
$InstallToken = %q
$AcceptWindowDays = %d
$InstallRoot = Join-Path $env:USERPROFILE ".new-api-usage-reporter"
$BinaryPath = Join-Path $InstallRoot "usage-reporter.exe"
$ConfigPath = Join-Path $InstallRoot "config.json"
$RunnerPath = Join-Path $InstallRoot "run.ps1"
$LogPath = Join-Path $InstallRoot "scheduled-task.log"
$StatePath = Join-Path $InstallRoot "state.json"
$CodexDir = Join-Path $env:USERPROFILE ".codex\sessions"
$TaskName = "new-api-usage-reporter-codex"
$BinUrl = "$ServiceUrl/api/external-usage/reporter/binary/windows-amd64/usage-reporter.exe"
$ExchangeUrl = "$ServiceUrl/api/external-usage/self/codex-installer/exchange"

New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null
$DeviceName = $env:COMPUTERNAME
$Hasher = [System.Security.Cryptography.SHA256]::Create()
$DeviceFingerprintBytes = [System.Text.Encoding]::UTF8.GetBytes("codex|windows-amd64|$DeviceName|$env:USERPROFILE")
$DeviceFingerprint = ([System.BitConverter]::ToString($Hasher.ComputeHash($DeviceFingerprintBytes))).Replace('-', '').ToLowerInvariant()

Invoke-WebRequest -Uri $BinUrl -OutFile $BinaryPath -UseBasicParsing

$ExchangePayload = @{
  install_token = $InstallToken
  device_name = $DeviceName
  device_fingerprint = $DeviceFingerprint
} | ConvertTo-Json -Compress

$ExchangeResponse = Invoke-RestMethod -Method Post -Uri $ExchangeUrl -ContentType "application/json" -Body $ExchangePayload
if (-not $ExchangeResponse.success) {
  throw "installer exchange failed"
}

$Server = $ExchangeResponse.data.server
if (-not $Server) {
  $Server = $ServiceUrl
}
$Source = $ExchangeResponse.data.source
if (-not $Source) {
  $Source = "codex"
}
$Credential = $ExchangeResponse.data.reporting_credential
if (-not $Credential) {
  throw "installer exchange returned empty reporting credential"
}

$Config = @{
  server = $Server
  source = $Source
  credential = $Credential
  codex_dir = $CodexDir
  state_path = $StatePath
  accept_window_days = $AcceptWindowDays
} | ConvertTo-Json -Compress

[System.IO.File]::WriteAllText($ConfigPath, $Config, (New-Object System.Text.UTF8Encoding($false)))

$RunnerScript = @"
& "$BinaryPath" --config "$ConfigPath" *>> "$LogPath"
"@
[System.IO.File]::WriteAllText($RunnerPath, $RunnerScript, (New-Object System.Text.UTF8Encoding($false)))

& $BinaryPath --config $ConfigPath

schtasks /Delete /TN $TaskName /F 2>$null | Out-Null
$TaskCommand = ('powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File "{0}"' -f $RunnerPath)
schtasks /Create /F /SC MINUTE /MO 5 /TN $TaskName /TR $TaskCommand | Out-Null

Write-Host "Installed Codex usage reporting to $InstallRoot"
`, serviceURL, installToken, params.AcceptWindowDays)
}

func usageReporterBinarySpecForTarget(target string) (*UsageReporterBinarySpec, error) {
	switch strings.TrimSpace(target) {
	case "darwin-arm64":
		return &UsageReporterBinarySpec{
			Target:       "darwin-arm64",
			GOOS:         "darwin",
			GOARCH:       "arm64",
			DownloadName: "usage-reporter",
		}, nil
	case "darwin-amd64":
		return &UsageReporterBinarySpec{
			Target:       "darwin-amd64",
			GOOS:         "darwin",
			GOARCH:       "amd64",
			DownloadName: "usage-reporter",
		}, nil
	case "windows-amd64":
		return &UsageReporterBinarySpec{
			Target:       "windows-amd64",
			GOOS:         "windows",
			GOARCH:       "amd64",
			DownloadName: "usage-reporter.exe",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported reporter binary target: %s", target)
	}
}

func usageReporterBinarySearchDirs() []string {
	dirs := make([]string, 0, 4)
	if configured := strings.TrimSpace(os.Getenv(usageReporterAssetDirEnv)); configured != "" {
		dirs = append(dirs, configured)
	}
	dirs = append(dirs, "/reporter-binaries")
	if executable, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(executable), "reporter-binaries"))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, "reporter-binaries"))
	}
	return dirs
}

func buildUsageReporterBinary(spec *UsageReporterBinarySpec) (string, error) {
	goBinary, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("reporter binary asset not available for %s", spec.Target)
	}
	projectRoot, err := findProjectRoot()
	if err != nil {
		return "", fmt.Errorf("reporter binary asset not available for %s", spec.Target)
	}

	outputDir := filepath.Join(os.TempDir(), usageReporterBinaryPrefix, spec.Target)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	outputPath := filepath.Join(outputDir, spec.DownloadName)
	cmd := exec.Command(goBinary, "build", "-o", outputPath, "./cmd/usage-reporter")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+spec.GOOS,
		"GOARCH="+spec.GOARCH,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build usage-reporter %s: %w (%s)", spec.Target, err, strings.TrimSpace(string(output)))
	}
	return outputPath, nil
}

func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current := cwd
	for {
		if info, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil && !info.IsDir() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("project root not found")
		}
		current = parent
	}
}

func ValidateInstallerPlatform(platform string) error {
	switch strings.TrimSpace(platform) {
	case ExternalUsageInstallerPlatformMacOS, ExternalUsageInstallerPlatformWindows:
		return nil
	default:
		return fmt.Errorf("unsupported installer platform: %s", platform)
	}
}

func validateActiveCodexInstallTokenTx(tx *gorm.DB, rawToken string) (*model.ExternalUsageInstallToken, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, model.ErrExternalUsageInstallTokenNotFound
	}
	installToken, err := model.GetExternalUsageInstallTokenByHash(tx, sha256Hex(rawToken))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.ErrExternalUsageInstallTokenNotFound
		}
		return nil, err
	}
	now := common.GetTimestamp()
	if installToken.Status == model.ExternalUsageStatusConsumed || installToken.ConsumedAt > 0 {
		return nil, model.ErrExternalUsageInstallTokenConsumed
	}
	if installToken.ExpiresAt > 0 && installToken.ExpiresAt < now {
		return nil, model.ErrExternalUsageInstallTokenExpired
	}
	if installToken.Source != model.ExternalUsageSourceCodex {
		return nil, errors.New("unsupported install token source")
	}
	return installToken, nil
}
