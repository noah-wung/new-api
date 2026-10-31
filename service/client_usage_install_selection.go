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
	if tokens.Codex == "" && tokens.Claude == "" && tokens.ZCode == "" {
		return "", errors.New("select at least one client collector")
	}
	if params.Managed && !SignedClientInstallerAvailable(params.ServiceURL) {
		return "", errors.New("Automatic updates require HTTPS and an active signed release.")
	}
	if tokens.ZCode != "" && !params.Managed {
		return "", errors.New("ZCode requires a signed managed installer")
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
		{"ZCode", "zcode_token", tokens.ZCode},
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
if ([Console]::IsInputRedirected -or [Console]::IsOutputRedirected) {
  throw 'Run the installation command in an interactive terminal.'
}
$Cursor = 0
$Cancelled = $false
$CursorVisible = [Console]::CursorVisible
$ControlCAsInput = [Console]::TreatControlCAsInput
$MenuHeight = $Collectors.Count + 1
Write-Host ([Environment]::NewLine * $MenuHeight) -NoNewline
$MenuTop = [Math]::Max(0, [Console]::CursorTop - $MenuHeight)
try {
  [Console]::CursorVisible = $false
  [Console]::TreatControlCAsInput = $true
  while ($true) {
    [Console]::SetCursorPosition(0, $MenuTop)
    $Width = [Math]::Max(1, [Console]::WindowWidth - 1)
    for ($Index = 0; $Index -lt $Collectors.Count; $Index++) {
      $Mark = ' '
      $Pointer = ' '
      if ($Collectors[$Index].Chosen) { $Mark = 'x' }
      if ($Index -eq $Cursor) { $Pointer = '>' }
      $Line = '{0} [{1}] {2}' -f $Pointer, $Mark, $Collectors[$Index].Label
      if ($Line.Length -gt $Width) { $Line = $Line.Substring(0, $Width) }
      Write-Host $Line.PadRight($Width)
    }
    $HelpLine = 'Up/Down: move  Space: select  Enter: install  q: cancel'
    if ($HelpLine.Length -gt $Width) { $HelpLine = $HelpLine.Substring(0, $Width) }
    Write-Host $HelpLine.PadRight($Width)
    $Key = [Console]::ReadKey($true)
    if ($Key.Key -eq [ConsoleKey]::UpArrow) { $Cursor = ($Cursor + $Collectors.Count - 1) % $Collectors.Count }
    elseif ($Key.Key -eq [ConsoleKey]::DownArrow) { $Cursor = ($Cursor + 1) % $Collectors.Count }
    elseif ($Key.Key -eq [ConsoleKey]::Spacebar) { $Collectors[$Cursor].Chosen = -not $Collectors[$Cursor].Chosen }
    elseif ($Key.Key -eq [ConsoleKey]::Enter) { break }
    elseif ($Key.Key -eq [ConsoleKey]::Q -or $Key.KeyChar -eq [char]3) { $Cancelled = $true; break }
  }
} finally {
  [Console]::TreatControlCAsInput = $ControlCAsInput
  [Console]::CursorVisible = $CursorVisible
  [Console]::SetCursorPosition(0, [Math]::Min([Console]::BufferHeight - 1, $MenuTop + $MenuHeight))
}
if ($Cancelled) { Write-Host 'Installation cancelled. No changes made.'; return }
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
	fmt.Fprintf(&script, `#!/bin/bash
set -eu
echo 'Choose collectors to install or add. Existing collectors remain installed.'
INSTALL_URL=%s
SELECTED=0
CURSOR=1
TTY_MODE=0
MENU_DRAWN=0
exec 3<&0
if [ -t 3 ]; then
  TTY_MODE=1
  TERMINAL_STATE="$(stty -g <&3)"
fi
restore_selector_terminal() {
  if [ "$TTY_MODE" -eq 1 ]; then
    stty "$TERMINAL_STATE" <&3 || :
    printf '\033[?25h'
  fi
}
trap restore_selector_terminal EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP
if [ "$TTY_MODE" -eq 1 ]; then
  # Read Ctrl+C as an explicit cancel key; restore signal processing on exit.
  stty -echo -icanon -isig min 1 time 0 <&3
  printf '\033[?25l'
fi
`, quoteInstallerLiteral(endpoint))
	for index := range choices {
		fmt.Fprintf(&script, "CHOSEN_%d=0\n", index+1)
	}
	fmt.Fprintf(&script, `while :; do
  if [ "$TTY_MODE" -eq 1 ] && [ "$MENU_DRAWN" -eq 1 ]; then printf '\033[%dA'; fi
`, len(choices)+1)
	for index, choice := range choices {
		number := index + 1
		fmt.Fprintf(&script, `  if [ "$CHOSEN_%d" -eq 1 ]; then MARK=x; else MARK=' '; fi
  if [ "$CURSOR" -eq %d ]; then POINTER='>'; else POINTER=' '; fi
  if [ "$TTY_MODE" -eq 1 ]; then printf '\r\033[2K'; fi
  printf '%%s [%%s] %%s\n' "$POINTER" "$MARK" %s
`, number, number, quoteInstallerLiteral(choice.Name))
	}
	script.WriteString(`  if [ "$TTY_MODE" -eq 1 ]; then printf '\r\033[2K'; fi
  printf 'Up/Down: move  Space: select  Enter: install  q: cancel\n'
  MENU_DRAWN=1
  KEY=''
  if ! IFS= read -r -s -n 1 KEY <&3; then
    echo 'Installation cancelled: no input. Run the command in an interactive terminal.' >&2
    exit 1
  fi
  case "$KEY" in
    q|Q) echo 'Installation cancelled. No changes made.'; exit 0 ;;
    ''|$'\r') break ;;
    $'\003') exit 130 ;;
    $'\004') echo 'Installation cancelled: no input.' >&2; exit 1 ;;
    $'\033')
      PREFIX=''
      SUFFIX=''
      if IFS= read -r -s -n 1 -t 1 PREFIX <&3 && { [ "$PREFIX" = '[' ] || [ "$PREFIX" = 'O' ]; }; then
        IFS= read -r -s -n 1 -t 1 SUFFIX <&3 || :
      fi
      case "$SUFFIX" in
`)
	fmt.Fprintf(&script, `        A) CURSOR=$(((CURSOR + %d - 2) %% %d + 1)) ;;
        B) CURSOR=$((CURSOR %% %d + 1)) ;;
`, len(choices), len(choices), len(choices))
	script.WriteString(`      esac
      ;;
    ' ')
      case "$CURSOR" in
`)
	for index := range choices {
		number := index + 1
		fmt.Fprintf(&script, "        %d) CHOSEN_%d=$((1 - CHOSEN_%d)) ;;\n", number, number, number)
	}
	script.WriteString("      esac\n      ;;\n  esac\ndone\nrestore_selector_terminal\ntrap - EXIT INT TERM HUP\nexec 3<&-\n")
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
