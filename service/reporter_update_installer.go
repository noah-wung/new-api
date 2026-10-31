package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
)

func SignedClientInstallerAvailable(serviceURL string) bool {
	origin, err := reporterupdate.Origin(serviceURL)
	if err != nil || !strings.HasPrefix(origin, "https://") {
		return false
	}
	_, _, err = ActiveReporterCatalog()
	return err == nil
}

// The first bootstrap is delivered over trusted HTTPS. The downloaded program
// verifies its own bytes against the signed catalog before redeeming tokens.
func buildManagedClientInstallScript(params BuildInstallScriptParams, tokens ClientInstallTokens) (string, error) {
	if e := ValidateInstallerPlatform(params.Platform); e != nil {
		return "", e
	}
	if tokens.Codex == "" && tokens.Claude == "" && tokens.ZCode == "" {
		return "", errors.New("select at least one client collector")
	}
	if !strings.HasPrefix(params.ServiceURL, "https://") {
		return "", errors.New("managed installation requires HTTPS")
	}
	data, c, e := ActiveReporterCatalog()
	if e != nil {
		return "", e
	}
	keys, e := ReporterTrustKeys()
	if e != nil {
		return "", e
	}
	keyData, e := common.Marshal(keys)
	if e != nil {
		return "", e
	}
	server, e := reporterupdate.Origin(params.ServiceURL)
	if e != nil {
		return "", e
	}
	encodedCatalog := base64.StdEncoding.EncodeToString(data)
	encodedKeys := base64.StdEncoding.EncodeToString(keyData)
	zcodeCheck, zcodeArg := "", ""
	if tokens.ZCode != "" {
		zcodeCheck = `"$INSTALL_TMP/usage-reporter" --supports-source zcode || { echo 'Collector does not support ZCode' >&2; exit 1; }`
		zcodeArg = " --zcode-install-token " + quoteInstallerLiteral(tokens.ZCode)
	}
	if params.Platform == ExternalUsageInstallerPlatformMacOS {
		arm, e := c.Artifact("darwin-arm64")
		if e != nil {
			return "", e
		}
		amd, e := c.Artifact("darwin-amd64")
		if e != nil {
			return "", e
		}
		return fmt.Sprintf(`#!/bin/sh
set -eu
umask 077
INSTALL_TMP="$(mktemp -d)"
trap 'rm -rf "$INSTALL_TMP"' EXIT INT TERM
printf '%%s' %s | /usr/bin/base64 -D > "$INSTALL_TMP/catalog.json"
printf '%%s' %s | /usr/bin/base64 -D > "$INSTALL_TMP/keys.json"
case "$(uname -m)" in
arm64|aarch64) ASSET=%s; EXPECTED=%s ;;
x86_64|amd64) ASSET=%s; EXPECTED=%s ;;
*) echo 'Unsupported macOS architecture' >&2; exit 1 ;;
esac
curl -fsSL %s"$ASSET" -o "$INSTALL_TMP/usage-reporter"
ACTUAL="$(shasum -a 256 "$INSTALL_TMP/usage-reporter" | awk '{print $1}')"
[ "$ACTUAL" = "$EXPECTED" ] || { echo 'Collector download checksum failed' >&2; exit 1; }
chmod 700 "$INSTALL_TMP/usage-reporter"
%s
"$INSTALL_TMP/usage-reporter" --install-clients --server %s --codex-install-token %s --claude-install-token %s%s --trust-keys "$INSTALL_TMP/keys.json" --catalog "$INSTALL_TMP/catalog.json" --install-auto-update=%t
`, quoteInstallerLiteral(encodedCatalog), quoteInstallerLiteral(encodedKeys), quoteInstallerLiteral(arm.Path), quoteInstallerLiteral(arm.SHA256), quoteInstallerLiteral(amd.Path), quoteInstallerLiteral(amd.SHA256), quoteInstallerLiteral(server), zcodeCheck, quoteInstallerLiteral(server), quoteInstallerLiteral(tokens.Codex), quoteInstallerLiteral(tokens.Claude), zcodeArg, params.AutoUpdate), nil
	}
	a, e := c.Artifact("windows-amd64")
	if e != nil {
		return "", e
	}
	winCheck, winArg := "", ""
	if tokens.ZCode != "" {
		winCheck = "$Check = Start-Process -FilePath $Binary -ArgumentList '--supports-source zcode' -WindowStyle Hidden -Wait -PassThru; if ($Check.ExitCode -ne 0) { throw 'Collector does not support ZCode' }"
		winArg = " --zcode-install-token \"" + strings.ReplaceAll(tokens.ZCode, "\"", "") + "\""
	}
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$InstallTmp = Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $InstallTmp | Out-Null
try {
  [IO.File]::WriteAllBytes((Join-Path $InstallTmp 'catalog.json'), [Convert]::FromBase64String('%s'))
  [IO.File]::WriteAllBytes((Join-Path $InstallTmp 'keys.json'), [Convert]::FromBase64String('%s'))
  $Binary = Join-Path $InstallTmp 'usage-reporter.exe'
  Invoke-WebRequest -Uri %s -OutFile $Binary -UseBasicParsing
  if ((Get-FileHash -LiteralPath $Binary -Algorithm SHA256).Hash.ToLowerInvariant() -ne '%s') { throw 'Collector download checksum failed' }
  %s
  $Log = Join-Path $env:USERPROFILE '.new-api-usage-reporter-install.log'
  $Arguments = ('--install-clients --server "{0}" --codex-install-token "{1}" --claude-install-token "{2}"%s --trust-keys "{3}" --catalog "{4}" --install-auto-update=%t --log-file "{5}"' -f %s, %s, %s, (Join-Path $InstallTmp 'keys.json'), (Join-Path $InstallTmp 'catalog.json'), $Log)
  $Run = Start-Process -FilePath $Binary -ArgumentList $Arguments -WindowStyle Hidden -Wait -PassThru
  if ($Run.ExitCode -ne 0) { throw "Collector installation failed. See $Log" }
  Write-Host 'Selected collectors installed. Restart newly configured Claude Code sessions.'
} finally { Remove-Item -LiteralPath $InstallTmp -Recurse -Force }
`, encodedCatalog, encodedKeys, "'"+strings.ReplaceAll(server+a.Path, "'", "''")+"'", a.SHA256, winCheck, winArg, params.AutoUpdate, "'"+strings.ReplaceAll(server, "'", "''")+"'", "'"+strings.ReplaceAll(tokens.Codex, "'", "''")+"'", "'"+strings.ReplaceAll(tokens.Claude, "'", "''")+"'"), nil
}
