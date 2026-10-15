package service

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf16"
)

func buildClaudeInstallerCommand(params BuildInstallScriptParams) (string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(params.ServiceURL), "/") + "/api/external-usage/reporter/install-claude."
	if params.Platform == ExternalUsageInstallerPlatformMacOS {
		endpoint += "sh?token=" + url.QueryEscape(params.InstallToken)
		return fmt.Sprintf(`bash -c "$(curl -fsSL %s)"`, quoteInstallerLiteral(endpoint)), nil
	}
	endpoint += "ps1?token=" + url.QueryEscape(params.InstallToken)
	command := "iwr '" + strings.ReplaceAll(endpoint, "'", "''") + "' -UseBasicParsing | iex"
	units := utf16.Encode([]rune(command))
	encoded := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], unit)
	}
	return "powershell -ExecutionPolicy Bypass -EncodedCommand " + base64.StdEncoding.EncodeToString(encoded), nil
}

func quoteInstallerLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func buildClaudeInstallScript(params BuildInstallScriptParams) string {
	server := strings.TrimRight(strings.TrimSpace(params.ServiceURL), "/")
	if params.Platform == ExternalUsageInstallerPlatformWindows {
		return strings.NewReplacer("__SERVER__", "'"+strings.ReplaceAll(server, "'", "''")+"'", "__TOKEN__", "'"+strings.ReplaceAll(params.InstallToken, "'", "''")+"'").Replace(claudeWindowsInstallScript)
	}
	return strings.NewReplacer("__SERVER__", quoteInstallerLiteral(server), "__TOKEN__", quoteInstallerLiteral(params.InstallToken)).Replace(claudeMacOSInstallScript)
}

const claudeMacOSInstallScript = `#!/bin/sh
set -eu
umask 077
SERVICE_URL=__SERVER__
INSTALL_TOKEN=__TOKEN__
INSTALL_ROOT="$HOME/.new-api-usage-reporter/claude"
BIN_PATH="$INSTALL_ROOT/usage-reporter"
CONFIG_PATH="$INSTALL_ROOT/config.json"
LABEL="pro.newapi.usage-reporter.claude"
PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM
mkdir -p "$INSTALL_ROOT" "$(dirname "$PLIST_PATH")"
case "$(uname -m)" in
  arm64|aarch64) TARGET="darwin-arm64" ;;
  x86_64|amd64) TARGET="darwin-amd64" ;;
  *) echo "unsupported macOS architecture" >&2; exit 1 ;;
esac
NEXT_BINARY="$TMP_DIR/usage-reporter"
curl -fsSL "$SERVICE_URL/api/external-usage/reporter/binary/$TARGET/usage-reporter?source=claude_code" -o "$NEXT_BINARY"
chmod 700 "$NEXT_BINARY"
"$NEXT_BINARY" --install-claude --server "$SERVICE_URL" --install-token "$INSTALL_TOKEN" --config "$CONFIG_PATH"

# Configuration must succeed before replacing an existing running agent.
launchctl bootout "gui/$(id -u)/$LABEL" >/dev/null 2>&1 || true
mv "$NEXT_BINARY" "$BIN_PATH"
escape_xml() {
  printf '%s' "$1" | sed 's/\&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g; s/'"'"'/\&apos;/g'
}
XML_BIN="$(escape_xml "$BIN_PATH")"
XML_CONFIG="$(escape_xml "$CONFIG_PATH")"
XML_LOG="$(escape_xml "$INSTALL_ROOT/receiver.log")"
cat > "$PLIST_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>$LABEL</string>
<key>ProgramArguments</key><array><string>$XML_BIN</string><string>--config</string><string>$XML_CONFIG</string><string>--serve</string></array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>$XML_LOG</string>
<key>StandardErrorPath</key><string>$XML_LOG</string>
</dict></plist>
EOF
launchctl enable "gui/$(id -u)/$LABEL"
launchctl bootstrap "gui/$(id -u)" "$PLIST_PATH"
echo "Claude Code usage reporter installed. Restart Claude Code CLI and VS Code sessions."
`

const claudeWindowsInstallScript = `$ErrorActionPreference = 'Stop'
$ServiceUrl = __SERVER__
$InstallToken = __TOKEN__
$InstallRoot = Join-Path $env:USERPROFILE '.new-api-usage-reporter\claude'
$BinaryPath = Join-Path $InstallRoot 'usage-reporter.exe'
$ConfigPath = Join-Path $InstallRoot 'config.json'
$TaskName = 'new-api-claude-usage-reporter'
$LogPath = Join-Path $InstallRoot 'scheduled-task.log'
$InstallLogPath = Join-Path $InstallRoot 'install.log'
$TempBinary = Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString() + '.exe')
New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null
try {
  Invoke-WebRequest -Uri "$ServiceUrl/api/external-usage/reporter/binary/windows-amd64/usage-reporter.exe?source=claude_code" -OutFile $TempBinary -UseBasicParsing
  $InstallArguments = ('--install-claude --server "{0}" --install-token "{1}" --config "{2}" --log-file "{3}"' -f $ServiceUrl, $InstallToken, $ConfigPath, $InstallLogPath)
  $InstallRun = Start-Process -FilePath $TempBinary -ArgumentList $InstallArguments -WindowStyle Hidden -Wait -PassThru
  if ($InstallRun.ExitCode -ne 0) { throw "Claude configuration failed; existing scheduled task was not replaced. See $InstallLogPath" }
  $PreviousTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
  if ($PreviousTask) {
    Stop-ScheduledTask -TaskName $TaskName
    $Deadline = (Get-Date).AddSeconds(15)
    while ((Get-ScheduledTask -TaskName $TaskName).State -eq 'Running') {
      if ((Get-Date) -gt $Deadline) { throw 'Previous Claude reporter did not stop' }
      Start-Sleep -Milliseconds 200
    }
  }
  Move-Item -LiteralPath $TempBinary -Destination $BinaryPath -Force
  $User = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
  $Action = New-ScheduledTaskAction -Execute $BinaryPath -Argument ('--config "{0}" --serve --log-file "{1}"' -f $ConfigPath, $LogPath)
  $Trigger = New-ScheduledTaskTrigger -AtLogOn -User $User
  $Principal = New-ScheduledTaskPrincipal -UserId $User -LogonType Interactive -RunLevel Limited
  $Settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
  Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings -Force | Out-Null
  Start-ScheduledTask -TaskName $TaskName
} finally {
  if (Test-Path -LiteralPath $TempBinary) { Remove-Item -LiteralPath $TempBinary -Force }
}
Write-Host 'Claude Code usage reporter installed. Restart Claude Code CLI and VS Code sessions.'
`
