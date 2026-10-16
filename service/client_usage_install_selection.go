package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type clientInstallChoice struct {
	Name, TokenParameter, Token string
}

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
	var choices []clientInstallChoice
	for _, choice := range []clientInstallChoice{
		{"Codex", "codex_token", tokens.Codex},
		{"Claude Code (CLI and VS Code)", "claude_token", tokens.Claude},
	} {
		if choice.Token != "" {
			choices = append(choices, choice)
		}
	}
	return renderClientSourceSelector(params.Platform, endpoint, choices), nil
}

// Both platforms render the same ordered choices. The selector has no knowledge
// of source identities or combinations, so additional adapters add ordinary rows.
func renderClientSourceSelector(platform, endpoint string, choices []clientInstallChoice) string {
	var script strings.Builder
	if platform == ExternalUsageInstallerPlatformWindows {
		fmt.Fprintf(&script, `$ErrorActionPreference = 'Stop'
Write-Host 'Choose collectors to install or add. Existing collectors remain installed.'
$InstallUrl = '%s'
$Collectors = @(
`, strings.ReplaceAll(endpoint, "'", "''"))
		for _, choice := range choices {
			fragment := "&" + url.Values{choice.TokenParameter: {choice.Token}}.Encode()
			fmt.Fprintf(&script, "  @{ Label = '%s'; Query = '%s'; Chosen = $false }\n", strings.ReplaceAll(choice.Name, "'", "''"), strings.ReplaceAll(fragment, "'", "''"))
		}
		script.WriteString(`)
while ($true) {
  Write-Host ''
  for ($Index = 0; $Index -lt $Collectors.Count; $Index++) {
    $Mark = ' '
    if ($Collectors[$Index].Chosen) { $Mark = 'x' }
    Write-Host ('[{0}] {1}. {2}' -f $Mark, ($Index + 1), $Collectors[$Index].Label)
  }
  $Answer = Read-Host 'Numbers toggle choices (e.g. 1,2); Enter installs; q cancels'
  if ($null -eq $Answer) { throw 'Installation cancelled: no input. Run the command in an interactive terminal.' }
  $Answer = $Answer.Trim()
  if ($Answer -eq 'q') { Write-Host 'Installation cancelled. No changes made.'; return }
  if ($Answer -eq '') { break }
  $Numbers = @($Answer -split '[,\s]+' | Where-Object { $_ -ne '' })
  $Valid = $Numbers.Count -gt 0
  foreach ($Number in $Numbers) {
    $Index = 0
    if ($Number -notmatch '^[1-9][0-9]*$' -or -not [int]::TryParse($Number, [ref]$Index) -or $Index -gt $Collectors.Count) { $Valid = $false; break }
  }
  if (-not $Valid) { Write-Host 'Enter only the displayed choice numbers.'; continue }
  foreach ($Number in $Numbers) {
    $Index = [int]$Number - 1
    $Collectors[$Index].Chosen = -not $Collectors[$Index].Chosen
  }
}
$Selected = @($Collectors | Where-Object { $_.Chosen })
if ($Selected.Count -eq 0) {
  Write-Host 'No collectors selected. No changes made.'
} else {
  foreach ($Collector in $Selected) { $InstallUrl += $Collector.Query }
  $InstallScript = (Invoke-WebRequest -Uri $InstallUrl -UseBasicParsing).Content
  & ([scriptblock]::Create($InstallScript))
}
`)
		return script.String()
	}
	fmt.Fprintf(&script, `#!/bin/sh
set -eu
set -f
echo 'Choose collectors to install or add. Existing collectors remain installed.'
INSTALL_URL=%s
SELECTED=0
`, quoteInstallerLiteral(endpoint))
	for index := range choices {
		fmt.Fprintf(&script, "CHOSEN_%d=0\n", index+1)
	}
	script.WriteString("while :; do\n  printf '\\n'\n")
	var numbers []string
	for index, choice := range choices {
		number := index + 1
		numbers = append(numbers, fmt.Sprint(number))
		fmt.Fprintf(&script, `  if [ "$CHOSEN_%d" -eq 1 ]; then MARK=x; else MARK=' '; fi
  printf '[%%s] %d. %%s\n' "$MARK" %s
`, number, number, quoteInstallerLiteral(choice.Name))
	}
	script.WriteString(`  printf 'Numbers toggle choices (e.g. 1,2); Enter installs; q cancels: '
  if ! IFS= read -r ANSWER; then
    echo 'Installation cancelled: no input. Run the command in an interactive terminal.' >&2
    exit 1
  fi
  case "$ANSWER" in
    q|Q) echo 'Installation cancelled. No changes made.'; exit 0 ;;
    '') break ;;
    *[!0-9,\ ]*) echo 'Enter only the displayed choice numbers.'; continue ;;
  esac
  NUMBERS="$(printf '%s' "$ANSWER" | tr ',' ' ')"
  VALID=0
  for NUMBER in $NUMBERS; do
    case "$NUMBER" in
`)
	fmt.Fprintf(&script, "      %s) VALID=1 ;;\n", strings.Join(numbers, "|"))
	script.WriteString(`      *) VALID=0; break ;;
    esac
  done
  if [ "$VALID" -eq 0 ]; then echo 'Enter only the displayed choice numbers.'; continue; fi
  for NUMBER in $NUMBERS; do
    case "$NUMBER" in
`)
	for index := range choices {
		number := index + 1
		fmt.Fprintf(&script, "      %d) CHOSEN_%d=$((1 - CHOSEN_%d)) ;;\n", number, number, number)
	}
	script.WriteString("    esac\n  done\ndone\n")
	for index, choice := range choices {
		fragment := "&" + url.Values{choice.TokenParameter: {choice.Token}}.Encode()
		fmt.Fprintf(&script, `if [ "$CHOSEN_%d" -eq 1 ]; then
  INSTALL_URL="$INSTALL_URL"%s
  SELECTED=$((SELECTED + 1))
fi
`, index+1, quoteInstallerLiteral(fragment))
	}
	script.WriteString(`if [ "$SELECTED" -eq 0 ]; then
  echo 'No collectors selected. No changes made.'
  exit 0
fi
INSTALL_SCRIPT="$(curl -fsSL "$INSTALL_URL")" || exit 1
sh -c "$INSTALL_SCRIPT"
`)
	return script.String()
}
