package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/model"
)

// ClientInstallTokens retain each source's authorization and one-time exchange.
// A combined installer must never mix tokens belonging to different users.
type ClientInstallTokens struct {
	Codex  string
	Claude string
}

func ValidateClientInstallTokens(tokens ClientInstallTokens) error {
	if tokens.Codex == "" && tokens.Claude == "" {
		return errors.New("select at least one client collector")
	}
	userID := -1
	for _, source := range []string{model.ExternalUsageSourceCodex, model.ExternalUsageSourceClaudeCode} {
		raw := tokens.Codex
		if source == model.ExternalUsageSourceClaudeCode {
			raw = tokens.Claude
		}
		if raw == "" {
			continue
		}
		if err := ValidateExternalUsageSourceEnabled(source); err != nil {
			return err
		}
		token, err := validateActiveUsageInstallTokenTx(model.DB, raw, source)
		if err != nil {
			return err
		}
		if userID != -1 && userID != token.UserID {
			return errors.New("client installer tokens must belong to the same user")
		}
		userID = token.UserID
	}
	return nil
}

func BuildClientInstallerCommand(params BuildInstallScriptParams, tokens ClientInstallTokens) (string, error) {
	if err := ValidateInstallerPlatform(params.Platform); err != nil {
		return "", err
	}
	if tokens.Codex == "" && tokens.Claude == "" {
		return "", errors.New("select at least one client collector")
	}
	query := url.Values{}
	if params.Managed { query.Set("auto_update",fmt.Sprint(params.AutoUpdate)) }
	if tokens.Codex != "" {
		query.Set("codex_token", tokens.Codex)
	}
	if tokens.Claude != "" {
		query.Set("claude_token", tokens.Claude)
	}
	extension := "sh"
	if params.Platform == ExternalUsageInstallerPlatformWindows {
		extension = "ps1"
	}
	endpoint := strings.TrimRight(strings.TrimSpace(params.ServiceURL), "/") + "/api/external-usage/reporter/install-clients." + extension + "?" + query.Encode()
	if params.Platform == ExternalUsageInstallerPlatformWindows {
		return buildWindowsInstallerCommand(endpoint), nil
	}
	return fmt.Sprintf(`bash -c "$(curl -fsSL %s)"`, quoteInstallerLiteral(endpoint)), nil
}

// BuildClientInstallScript downloads once, then runs the selected installers in
// isolated scopes. Each keeps its existing config, state, task and route policy.
func BuildClientInstallScript(params BuildInstallScriptParams, tokens ClientInstallTokens) (string, error) {
	if params.Managed { return buildManagedClientInstallScript(params,tokens) }
	if err := ValidateInstallerPlatform(params.Platform); err != nil {
		return "", err
	}
	if tokens.Codex == "" && tokens.Claude == "" {
		return "", errors.New("select at least one client collector")
	}
	source := model.ExternalUsageSourceCodex
	if tokens.Codex == "" {
		source = model.ExternalUsageSourceClaudeCode
	}
	server := strings.TrimRight(strings.TrimSpace(params.ServiceURL), "/")
	var script strings.Builder
	if params.Platform == ExternalUsageInstallerPlatformMacOS {
		fmt.Fprintf(&script, `#!/bin/sh
set -eu
umask 077
CLIENT_BUNDLE_DIR="$(mktemp -d)"
trap 'rm -rf "$CLIENT_BUNDLE_DIR"' EXIT INT TERM
export CLIENT_BUNDLE_BINARY="$CLIENT_BUNDLE_DIR/usage-reporter"
case "$(uname -m)" in
  arm64|aarch64) CLIENT_BUNDLE_TARGET="darwin-arm64" ;;
  x86_64|amd64) CLIENT_BUNDLE_TARGET="darwin-amd64" ;;
  *) echo "unsupported macOS architecture" >&2; exit 1 ;;
esac
CLIENT_BUNDLE_SERVER=%s
curl -fsSL "$CLIENT_BUNDLE_SERVER/api/external-usage/reporter/binary/$CLIENT_BUNDLE_TARGET/usage-reporter?source=%s" -o "$CLIENT_BUNDLE_BINARY"
chmod 700 "$CLIENT_BUNDLE_BINARY"
CLIENT_BUNDLE_FAILED=0
`, quoteInstallerLiteral(server), source)
	} else {
		fmt.Fprintf(&script, `$ErrorActionPreference = 'Stop'
$ClientBundleDir = Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $ClientBundleDir | Out-Null
$ClientBundleBinary = Join-Path $ClientBundleDir 'usage-reporter.exe'
$ClientBundleFailed = $false
try {
Invoke-WebRequest -Uri %s -OutFile $ClientBundleBinary -UseBasicParsing
`, "'"+strings.ReplaceAll(server+"/api/external-usage/reporter/binary/windows-amd64/usage-reporter.exe?source="+source, "'", "''")+"'")
	}
	for _, client := range []struct{ source, token, label string }{
		{model.ExternalUsageSourceCodex, tokens.Codex, "Codex"},
		{model.ExternalUsageSourceClaudeCode, tokens.Claude, "Claude Code"},
	} {
		if client.token == "" {
			continue
		}
		params.Source, params.InstallToken, params.BundledBinary = client.source, client.token, true
		child, err := BuildInstallScript(params)
		if err != nil {
			return "", err
		}
		if params.Platform == ExternalUsageInstallerPlatformMacOS {
			// A separate shell preserves set -e even when the parent handles failure.
			fmt.Fprintf(&script, "if sh <<'NEW_API_CLIENT_INSTALLER'\n%s\nNEW_API_CLIENT_INSTALLER\nthen\n  echo '%s installation completed'\nelse\n  echo '%s installation failed; generate a new command to retry' >&2\n  CLIENT_BUNDLE_FAILED=1\nfi\n", child, client.label, client.label)
		} else {
			fmt.Fprintf(&script, "try {\n& {\n%s\n}\n} catch {\nWrite-Warning '%s installation failed; check its logs and generate a new command to retry'\n$ClientBundleFailed = $true\n}\n", child, client.label)
		}
	}
	if params.Platform == ExternalUsageInstallerPlatformMacOS {
		script.WriteString("exit \"$CLIENT_BUNDLE_FAILED\"\n")
	} else {
		script.WriteString("if ($ClientBundleFailed) { throw 'Some client collectors were not installed; see the results above' }\n} finally {\nRemove-Item -LiteralPath $ClientBundleDir -Recurse -Force\n}\n")
	}
	return script.String(), nil
}
