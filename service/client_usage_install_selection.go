package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Selection happens before fetching the ordinary or signed installer. Only
// selected source tokens reach that installer and can be exchanged for devices.
func buildInteractiveClientInstallScript(params BuildInstallScriptParams, tokens ClientInstallTokens) (string, error) {
	if err := ValidateInstallerPlatform(params.Platform); err != nil {
		return "", err
	}
	if tokens.Codex == "" && tokens.Claude == "" {
		return "", errors.New("select at least one client collector")
	}
	if params.Managed && !SignedClientInstallerAvailable(params.ServiceURL) {
		return "", errors.New("Automatic updates require HTTPS and an active signed release.")
	}
	query := url.Values{}
	if params.Managed {
		query.Set("auto_update", fmt.Sprint(params.AutoUpdate))
	}
	extension := "sh"
	if params.Platform == ExternalUsageInstallerPlatformWindows {
		extension = "ps1"
	}
	endpoint := strings.TrimRight(strings.TrimSpace(params.ServiceURL), "/") + "/api/external-usage/reporter/install-clients." + extension + "?" + query.Encode()
	var script strings.Builder
	if params.Platform == ExternalUsageInstallerPlatformMacOS {
		fmt.Fprintf(&script, `#!/bin/sh
set -eu
echo 'Choose collectors to install or add. Existing collectors remain installed.'
INSTALL_URL=%s
SELECTED=0
`, quoteInstallerLiteral(endpoint))
	} else {
		fmt.Fprintf(&script, `$ErrorActionPreference = 'Stop'
Write-Host 'Choose collectors to install or add. Existing collectors remain installed.'
$InstallUrl = '%s'
$Selected = 0
`, strings.ReplaceAll(endpoint, "'", "''"))
	}
	for _, client := range []struct{ name, parameter, token string }{
		{"Codex", "codex_token", tokens.Codex},
		{"Claude Code (CLI and VS Code)", "claude_token", tokens.Claude},
	} {
		if client.token == "" {
			continue
		}
		fragment := "&" + url.Values{client.parameter: {client.token}}.Encode()
		if params.Platform == ExternalUsageInstallerPlatformMacOS {
			fmt.Fprintf(&script, `while :; do
  printf 'Collect usage from %s? [y/N]: '
  if ! IFS= read -r ANSWER; then
    echo 'Installation cancelled: no input. Run the command in an interactive terminal.' >&2
    exit 1
  fi
  case "$ANSWER" in
    y|Y|yes|YES) INSTALL_URL="$INSTALL_URL"%s; SELECTED=$((SELECTED + 1)); break ;;
    n|N|no|NO|'') break ;;
    *) echo 'Enter y or n.' ;;
  esac
done
`, client.name, quoteInstallerLiteral(fragment))
		} else {
			fmt.Fprintf(&script, `while ($true) {
  $Answer = Read-Host 'Collect usage from %s? [y/N]'
  if ($null -eq $Answer) { throw 'Installation cancelled: no input. Run the command in an interactive terminal.' }
  $Answer = $Answer.Trim()
  if ($Answer -match '^(?i:y|yes)$') { $InstallUrl += '%s'; $Selected++; break }
  if ($Answer -match '^(?i:n|no)?$') { break }
  Write-Host 'Enter y or n.'
}
`, client.name, strings.ReplaceAll(fragment, "'", "''"))
		}
	}
	if params.Platform == ExternalUsageInstallerPlatformMacOS {
		script.WriteString(`if [ "$SELECTED" -eq 0 ]; then
  echo 'No collectors selected. No changes made.'
  exit 0
fi
INSTALL_SCRIPT="$(curl -fsSL "$INSTALL_URL")" || exit 1
sh -c "$INSTALL_SCRIPT"
`)
	} else {
		script.WriteString(`if ($Selected -eq 0) {
  Write-Host 'No collectors selected. No changes made.'
} else {
  $InstallScript = (Invoke-WebRequest -Uri $InstallUrl -UseBasicParsing).Content
  & ([scriptblock]::Create($InstallScript))
}
`)
	}
	return script.String(), nil
}
