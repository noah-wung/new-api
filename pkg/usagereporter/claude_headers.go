package usagereporter

import (
	"os"
	"strings"
)

const ClaudeOriginHeader = "X-New-Api-Upstream-Origin"

// ClaudeTelemetryHeaders runs inside Claude's otelHeadersHelper subprocess.
// Only the normalized origin is returned: no API keys, paths or reporting
// credentials. Unknown routes explicitly replace previously cached headers.
func ClaudeTelemetryHeaders() map[string]string {
	upstream := os.Getenv("ANTHROPIC_BASE_URL")
	providers := 0
	for _, provider := range []struct{ flag, endpoint, fallback string }{
		{"CLAUDE_CODE_USE_BEDROCK", "ANTHROPIC_BEDROCK_BASE_URL", "https://bedrock-runtime.amazonaws.com"},
		{"CLAUDE_CODE_USE_VERTEX", "ANTHROPIC_VERTEX_BASE_URL", "https://aiplatform.googleapis.com"},
		{"CLAUDE_CODE_USE_FOUNDRY", "ANTHROPIC_FOUNDRY_BASE_URL", ""},
		{"CLAUDE_CODE_USE_ANTHROPIC_AWS", "ANTHROPIC_AWS_BASE_URL", "https://aws-external-anthropic.api.aws"},
	} {
		if value := strings.ToLower(os.Getenv(provider.flag)); value != "1" && value != "true" {
			continue
		}
		providers++
		upstream = os.Getenv(provider.endpoint)
		if upstream == "" {
			upstream = provider.fallback
		}
		// A provider default identifies a direct cloud route, not a manually
		// supplied gateway. Respect every supported custom endpoint first.
		if provider.flag == "CLAUDE_CODE_USE_BEDROCK" && os.Getenv("ANTHROPIC_BEDROCK_MANTLE_BASE_URL") != "" {
			// Mantle selection can depend on the model. Do not label a mixed
			// custom Mantle/runtime route as the public Bedrock endpoint.
			upstream = ""
		}
		if provider.flag == "CLAUDE_CODE_USE_FOUNDRY" && upstream == "" {
			resource := os.Getenv("ANTHROPIC_FOUNDRY_RESOURCE")
			if len(resource) >= 2 && len(resource) <= 64 && strings.Trim(resource, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") == "" {
				upstream = "https://" + resource + ".services.ai.azure.com"
			}
		}
	}
	if providers == 0 && upstream == "" {
		upstream = "https://api.anthropic.com"
	}
	origin, err := NormalizeUpstreamURL(upstream)
	if err != nil || providers > 1 {
		origin = "unknown"
	}
	return map[string]string{ClaudeOriginHeader: origin}
}
