package usagereporter

import "time"

type UsageEvent struct {
	EventID               string `json:"event_id"`
	SessionID             string `json:"session_id"`
	OccurredAt            int64  `json:"occurred_at"`
	SourceModelName       string `json:"source_model_name"`
	InputTokens           int    `json:"input_tokens"`
	CachedInputTokens     int    `json:"cached_input_tokens"`
	OutputTokens          int    `json:"output_tokens"`
	ReasoningOutputTokens int    `json:"reasoning_output_tokens"`
	TotalTokens           int    `json:"total_tokens"`
}

// CollectedEvent contains only normalized metadata. Origin stays on disk for
// policy rechecks and is never included in the HTTP usage payload.
type CollectedEvent struct {
	RouteID       string     `json:"route_id,omitempty"`
	Event         UsageEvent `json:"event"`
	ClientVersion string     `json:"client_version"`
	Origin        string     `json:"upstream_origin"`
}

type clientAcceptWindow struct {
	Enabled     bool
	CutoffUnix  int64
	FutureLimit int64
}

func buildClientAcceptWindow(acceptWindowDays int) clientAcceptWindow {
	if acceptWindowDays <= 0 {
		return clientAcceptWindow{}
	}
	now := time.Now()
	return clientAcceptWindow{
		Enabled:     true,
		CutoffUnix:  now.Add(-time.Duration(acceptWindowDays) * 24 * time.Hour).Unix(),
		FutureLimit: now.Unix() + 300,
	}
}
