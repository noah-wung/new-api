package usagereporter

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const SourceClaudeCode = "claude_code"

// ClaudeUpstreamAttribute is an operator declaration of the effective route.
// Claude Code does not itself attach a trustworthy upstream URL to API events.
const ClaudeUpstreamAttribute = "new_api.upstream_origin"

type otlpValue struct {
	StringValue string          `json:"stringValue"`
	IntValue    json.RawMessage `json:"intValue"`
	DoubleValue *float64        `json:"doubleValue"`
}

func (v otlpValue) text() string {
	if v.StringValue != "" {
		return v.StringValue
	}
	if v.IntValue != nil {
		return strings.Trim(string(v.IntValue), `"`)
	}
	return ""
}

type otlpAttribute struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpLogRecord struct {
	TimeUnixNano string          `json:"timeUnixNano"`
	Body         otlpValue       `json:"body"`
	EventName    string          `json:"eventName"`
	Attributes   []otlpAttribute `json:"attributes"`
}

type otlpLogsRequest struct {
	ResourceLogs []struct {
		Resource struct {
			Attributes []otlpAttribute `json:"attributes"`
		} `json:"resource"`
		ScopeLogs []struct {
			LogRecords []otlpLogRecord `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
}

type claudeQueuedEvent struct {
	Event         UsageEvent `json:"event"`
	ClientVersion string     `json:"client_version"`
	Origin        string     `json:"upstream_origin"`
}

type claudeRoutePolicy struct {
	gateways map[string]bool
	external map[string]bool
}

func newClaudeRoutePolicy(cfg Config) (*claudeRoutePolicy, error) {
	p := &claudeRoutePolicy{gateways: make(map[string]bool), external: make(map[string]bool)}
	for _, value := range append([]string{cfg.Server}, cfg.GatewayURLs...) {
		origin, err := claudeRouteOrigin(value)
		if err != nil {
			return nil, errors.New("invalid gateway URL in reporter configuration")
		}
		p.gateways[origin] = true
	}
	// An empty allowlist permits only the official API. Third-party gateways
	// require an explicit external route declaration, never a model-name guess.
	urls := cfg.ClaudeExternalURLs
	if urls == nil {
		urls = []string{"https://api.anthropic.com"}
	}
	for _, value := range urls {
		origin, err := claudeRouteOrigin(value)
		if err != nil {
			return nil, errors.New("invalid external Claude URL in reporter configuration")
		}
		p.external[origin] = true
	}
	return p, nil
}

func claudeRouteOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("route must be an HTTP(S) URL without credentials, query, or fragment")
	}
	return gatewayOrigin(value)
}

func claudeListenAddress(cfg Config) (string, error) {
	address := cfg.ClaudeListen
	if address == "" {
		address = "127.0.0.1:4318"
	}
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	number, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || portErr != nil || number < 1 || number > 65535 {
		return "", errors.New("claude_listen must be a numeric loopback IP and port (1-65535)")
	}
	return address, nil
}

// normalizeClaudeRequest retains only usage metadata; arbitrary attributes,
// event bodies and source identities never enter the durable queue. Only the
// normalized origin is retained locally so queued events can be re-filtered.
func normalizeClaudeRequest(record otlpLogRecord, resource []otlpAttribute, policy *claudeRoutePolicy, window clientAcceptWindow) (*claudeQueuedEvent, error) {
	attributes := make(map[string]otlpValue)
	for _, attr := range resource {
		attributes[attr.Key] = attr.Value
	}
	for _, attr := range record.Attributes {
		attributes[attr.Key] = attr.Value
	}
	name := record.EventName
	if name == "" {
		name = record.Body.StringValue
	}
	if name == "" {
		name = attributes["event.name"].text()
	}
	if name != "claude_code.api_request" && name != "api_request" {
		return nil, nil
	}
	origin, err := claudeRouteOrigin(attributes[ClaudeUpstreamAttribute].text())
	if err != nil {
		return nil, errors.New("missing or invalid upstream declaration")
	}
	if policy.gateways[origin] {
		return nil, nil
	}
	if !policy.external[origin] {
		return nil, errors.New("upstream is not an approved external route")
	}
	var timestamp time.Time
	if record.TimeUnixNano != "" {
		nanos, err := strconv.ParseInt(record.TimeUnixNano, 10, 64)
		if err != nil || nanos <= 0 {
			return nil, errors.New("invalid event time")
		}
		timestamp = time.Unix(0, nanos)
	} else {
		timestamp, err = time.Parse(time.RFC3339Nano, attributes["event.timestamp"].text())
		if err != nil {
			return nil, errors.New("missing or invalid event time")
		}
	}
	if timestamp.Unix() <= 0 || timestamp.After(time.Now().Add(5*time.Minute)) || window.Enabled && timestamp.Unix() < window.CutoffUnix {
		return nil, errors.New("event time is outside the acceptance window")
	}
	model := strings.TrimSpace(attributes["model"].text())
	if model == "" || len(model) > 128 {
		return nil, errors.New("missing or invalid model")
	}
	counts := make([]int, 4)
	for i, key := range []string{"input_tokens", "cache_read_tokens", "cache_creation_tokens", "output_tokens"} {
		value, present := attributes[key]
		if !present && (key == "cache_read_tokens" || key == "cache_creation_tokens") {
			continue
		}
		text := value.text()
		if value.DoubleValue != nil {
			text = strconv.FormatFloat(*value.DoubleValue, 'f', -1, 64)
		}
		n, err := strconv.ParseInt(text, 10, 32)
		if err != nil || n < 0 {
			return nil, errors.New("invalid token count")
		}
		counts[i] = int(n)
	}
	total := int64(counts[0]) + int64(counts[1]) + int64(counts[2]) + int64(counts[3])
	if total <= 0 || total > 2147483647 {
		return nil, errors.New("invalid token total")
	}
	session := attributes["session.id"].text()
	requestID := attributes["request_id"].text()
	identity := "request:" + requestID
	if requestID == "" {
		if clientID := attributes["client_request_id"].text(); clientID != "" {
			identity = "client:" + clientID
		} else {
			// Sequence alone repeats on process restart. Include the original event
			// time and normalized usage; re-exporting the same record stays stable.
			if session == "" {
				return nil, errors.New("event without request ID requires a session ID")
			}
			metadata, err := common.Marshal([]any{session, timestamp.UnixNano(), attributes["event.sequence"].text(), attributes["service.instance.id"].text(), model, counts})
			if err != nil {
				return nil, err
			}
			identity = "fallback:" + string(metadata)
		}
	}
	version := attributes["service.version"].text()
	if version == "" {
		version = attributes["app.version"].text()
	}
	if len(version) > 64 {
		version = ""
	}
	return &claudeQueuedEvent{
		Event: UsageEvent{
			EventID:   sha256Hex(SourceClaudeCode + ":" + origin + ":" + identity),
			SessionID: sha256Hex(session), OccurredAt: timestamp.Unix(), SourceModelName: model,
			InputTokens: counts[0] + counts[1] + counts[2], CachedInputTokens: counts[1],
			OutputTokens: counts[3], TotalTokens: int(total),
		},
		ClientVersion: version, Origin: origin,
	}, nil
}
