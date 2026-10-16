package usagereporter

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ConfigureClaudeTelemetry configures a logs-only exporter and a runtime helper.
// The helper inherits each Claude process's routing environment, including its
// effective settings. API routing itself is never changed by installation.
func ConfigureClaudeTelemetry(cfg Config, path, binaryPath string) error {
	address, err := claudeListenAddress(cfg)
	if err != nil {
		return err
	}
	if len(cfg.ClaudeReceiverToken) < 32 || strings.ContainsAny(cfg.ClaudeReceiverToken, " \t\r\n,") {
		return errors.New("configure a valid claude_receiver_token first")
	}
	if !filepath.IsAbs(binaryPath) || strings.ContainsAny(binaryPath, "\r\n") {
		return errors.New("Claude header helper requires an absolute binary path")
	}
	// Claude runs commands with arguments through the platform shell. Use
	// cmd.exe quoting on Windows and POSIX quoting elsewhere.
	helper := "'" + strings.ReplaceAll(binaryPath, "'", "'\"'\"'") + "' --claude-otel-headers"
	if runtime.GOOS == "windows" {
		if strings.ContainsAny(binaryPath, "%\"") {
			return errors.New("Claude helper path cannot contain shell expansion characters")
		}
		helper = "\"" + binaryPath + "\" --claude-otel-headers"
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
	if raw, exists := settings["otelHeadersHelper"]; exists {
		var current string
		if common.Unmarshal(raw, &current) != nil || current != "" && current != helper {
			return errors.New("Claude already has a different otelHeadersHelper; preserve it and merge telemetry configuration explicitly")
		}
	}
	settings["otelHeadersHelper"], err = common.Marshal(helper)
	if err != nil {
		return err
	}
	resourceAttrs := make([]string, 0)
	for _, item := range strings.Split(env["OTEL_RESOURCE_ATTRIBUTES"], ",") {
		key, _, _ := strings.Cut(strings.TrimSpace(item), "=")
		if key != "" && key != ClaudeUpstreamAttribute {
			resourceAttrs = append(resourceAttrs, item)
		}
	}
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
