package usagereporter

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ConfigureClaudeTelemetry merges a logs-only exporter into the user settings.
// upstream is an explicit routing assertion, never inferred from a model name.
// This function does not change the actual Anthropic API routing configuration.
func ConfigureClaudeTelemetry(cfg Config, path, upstream string) error {
	address, err := claudeListenAddress(cfg)
	if err != nil {
		return err
	}
	if len(cfg.ClaudeReceiverToken) < 32 || strings.ContainsAny(cfg.ClaudeReceiverToken, " \t\r\n,") {
		return errors.New("configure a valid claude_receiver_token first")
	}
	origin, err := claudeRouteOrigin(upstream)
	if err != nil || strings.ContainsAny(origin, ",= \t\r\n") {
		return errors.New("an explicit --claude-upstream URL is required")
	}
	policy, err := newClaudeRoutePolicy(cfg)
	if err != nil {
		return err
	}
	if !policy.external[origin] && !policy.gateways[origin] {
		return errors.New("declare the upstream in claude_external_urls or gateway_urls first")
	}
	settings := make(map[string]json.RawMessage)
	previous, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return errors.New("could not read Claude settings")
	}
	if len(previous) > 0 {
		if err := common.Unmarshal(previous, &settings); err != nil || settings == nil {
			return errors.New("invalid Claude settings JSON object")
		}
	}
	env := make(map[string]string)
	if raw, ok := settings["env"]; ok {
		if err := common.Unmarshal(raw, &env); err != nil || env == nil {
			return errors.New("Claude settings env must be an object of strings")
		}
	}
	// Validate known routing settings. Project/managed settings and launcher
	// overrides still require an operator-controlled, consistent route profile.
	configuredURL := env["ANTHROPIC_BASE_URL"]
	if configuredURL == "" {
		configuredURL = "https://api.anthropic.com"
	}
	configuredOrigin, err := claudeRouteOrigin(configuredURL)
	if err != nil || configuredOrigin != origin {
		return errors.New("declared upstream differs from Claude user settings; configure routing before telemetry")
	}
	if processURL := os.Getenv("ANTHROPIC_BASE_URL"); processURL != "" {
		processOrigin, err := claudeRouteOrigin(processURL)
		if err != nil || processOrigin != origin {
			return errors.New("declared upstream differs from the launch environment")
		}
	}
	for _, key := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		if env[key] == "1" || os.Getenv(key) == "1" {
			return errors.New("alternate provider routing requires an explicitly managed telemetry profile")
		}
	}
	resourceAttrs := make([]string, 0)
	for _, item := range strings.Split(env["OTEL_RESOURCE_ATTRIBUTES"], ",") {
		key, _, _ := strings.Cut(strings.TrimSpace(item), "=")
		if key != "" && key != ClaudeUpstreamAttribute {
			resourceAttrs = append(resourceAttrs, item)
		}
	}
	resourceAttrs = append(resourceAttrs, ClaudeUpstreamAttribute+"="+origin)
	env["OTEL_RESOURCE_ATTRIBUTES"] = strings.Join(resourceAttrs, ",")
	env["CLAUDE_CODE_ENABLE_TELEMETRY"] = "1"
	env["OTEL_LOGS_EXPORTER"] = "otlp"
	env["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"] = "http/json"
	env["OTEL_EXPORTER_OTLP_LOGS_COMPRESSION"] = "none"
	env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] = "http://" + address + "/v1/logs"
	env["OTEL_EXPORTER_OTLP_LOGS_HEADERS"] = "Authorization=Bearer " + url.QueryEscape(cfg.ClaudeReceiverToken)
	env["OTEL_METRICS_INCLUDE_ENTRYPOINT"] = "true"
	env["OTEL_METRICS_INCLUDE_RESOURCE_ATTRIBUTES"] = "true"
	for _, key := range []string{"OTEL_LOG_USER_PROMPTS", "OTEL_LOG_ASSISTANT_RESPONSES", "OTEL_LOG_TOOL_DETAILS", "OTEL_LOG_TOOL_CONTENT", "OTEL_LOG_RAW_API_BODIES"} {
		env[key] = "0"
	}
	settings["env"], err = common.Marshal(env)
	if err != nil {
		return err
	}
	content, err := common.Marshal(settings)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if len(previous) > 0 {
		backup, err := os.OpenFile(path+".before-new-api-telemetry", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := backup.Write(previous)
			closeErr := backup.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
		} else if !os.IsExist(err) {
			return err
		}
	}
	return writePrivateFile(path, content)
}
