package usagereporter

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const SourceCodex = "codex"

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

type CollectResult struct {
	ClientVersion string
	Events        []UsageEvent
	NextState     *State
}

type codexLineEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexSessionMetaPayload struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	CLIVersion string `json:"cli_version"`
}

type codexTurnContextPayload struct {
	TurnID string `json:"turn_id"`
	Model  string `json:"model"`
}

type codexEventMsgPayload struct {
	Type string `json:"type"`
	Info struct {
		TotalTokenUsage TokenUsageTotals `json:"total_token_usage"`
	} `json:"info"`
}

type pendingCodexUsageEvent struct {
	SessionHash           string
	OccurredAt            int64
	RunningTotalTokens    int
	InputTokens           int
	CachedInputTokens     int
	OutputTokens          int
	ReasoningOutputTokens int
	TotalTokens           int
}

func CollectCodexUsage(root string, state *State) (*CollectResult, error) {
	return CollectCodexUsageWithWindow(root, state, 0)
}

func CollectCodexUsageWithWindow(root string, state *State, acceptWindowDays int) (*CollectResult, error) {
	if state == nil {
		state = NewState()
	}
	nextState := state.Clone()
	nextState.SessionContextVersion = reporterStateSessionContextVersion
	window := buildClientAcceptWindow(acceptWindowDays)
	seenEventIDs := make(map[string]struct{})
	files, err := listCodexUsageFiles(root)
	if err != nil {
		return nil, err
	}
	result := &CollectResult{
		Events:    make([]UsageEvent, 0),
		NextState: nextState,
	}
	resetSessionTotals := make(map[string]struct{})
	for _, path := range files {
		fileEvents, fileVersion, fileState, err := collectCodexUsageFile(path, state, nextState, window, seenEventIDs, resetSessionTotals)
		if err != nil {
			return nil, err
		}
		if fileVersion != "" {
			result.ClientVersion = fileVersion
		}
		nextState.Files[path] = fileState
		result.Events = append(result.Events, fileEvents...)
	}
	if result.ClientVersion == "" {
		result.ClientVersion = "codex-reporter"
	}
	return result, nil
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

func collectCodexUsageFile(path string, current *State, next *State, window clientAcceptWindow, seenEventIDs map[string]struct{}, resetSessionTotals map[string]struct{}) ([]UsageEvent, string, FileState, error) {
	fileState := current.Files[path]
	sessionID := strings.TrimSpace(fileState.SessionID)
	cliVersion := fileState.CLIVersion
	turnID, model := restoreSessionContext(next, sessionID, fileState.TurnID, fileState.Model)
	offset := fileState.Offset
	if shouldForceCodexStateRescan(fileState, current) {
		offset = 0
		if sessionID != "" {
			sessionHash := sha256Hex(sessionID)
			if _, alreadyReset := resetSessionTotals[sessionHash]; !alreadyReset {
				delete(next.SessionTotals, sessionHash)
				resetSessionTotals[sessionHash] = struct{}{}
			}
		}
	}

	handle, err := os.Open(path)
	if err != nil {
		return nil, "", fileState, err
	}
	defer handle.Close()

	if offset > 0 {
		if _, err := handle.Seek(offset, io.SeekStart); err != nil {
			return nil, "", fileState, err
		}
	}

	reader := bufio.NewReader(handle)
	events := make([]UsageEvent, 0)
	pendingEvents := make([]pendingCodexUsageEvent, 0)
	currentOffset := offset
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			currentOffset += int64(len(line))
			trimmed := strings.TrimSpace(string(line))
			if trimmed == "" {
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, "", fileState, err
				}
				continue
			}
			var envelope codexLineEnvelope
			if unmarshalErr := common.Unmarshal([]byte(trimmed), &envelope); unmarshalErr != nil {
				return nil, "", fileState, unmarshalErr
			}
			switch envelope.Type {
			case "session_meta":
				var payload codexSessionMetaPayload
				if unmarshalErr := common.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
					return nil, "", fileState, unmarshalErr
				}
				newSessionID := strings.TrimSpace(payload.SessionID)
				if newSessionID == "" {
					newSessionID = strings.TrimSpace(payload.ID)
				}
				if newSessionID != "" {
					flushPendingCodexUsageEvents(&events, &pendingEvents, current, turnID, model, window, seenEventIDs)
					sessionID = newSessionID
					turnID, model = restoreSessionContext(next, sessionID, "", "")
				}
				if strings.TrimSpace(payload.CLIVersion) != "" {
					cliVersion = strings.TrimSpace(payload.CLIVersion)
				}
			case "turn_context":
				var payload codexTurnContextPayload
				if unmarshalErr := common.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
					return nil, "", fileState, unmarshalErr
				}
				turnID = strings.TrimSpace(payload.TurnID)
				model = strings.TrimSpace(payload.Model)
				flushPendingCodexUsageEvents(&events, &pendingEvents, current, turnID, model, window, seenEventIDs)
				persistSessionContext(next, sessionID, turnID, model)
			case "event_msg":
				var payload codexEventMsgPayload
				if unmarshalErr := common.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
					return nil, "", fileState, unmarshalErr
				}
				if payload.Type != "token_count" || strings.TrimSpace(sessionID) == "" {
					break
				}
				sessionHash := sha256Hex(sessionID)
				total := payload.Info.TotalTokenUsage
				prev := next.SessionTotals[sessionHash]
				delta := diffTotals(prev, total)
				next.SessionTotals[sessionHash] = mergeSessionTotals(prev, total)
				if delta.TotalTokens <= 0 {
					break
				}
				occurredAt := parseOccurredAt(envelope.Timestamp)
				if strings.TrimSpace(model) == "" {
					pendingEvents = append(pendingEvents, pendingCodexUsageEvent{
						SessionHash:           sessionHash,
						OccurredAt:            occurredAt,
						RunningTotalTokens:    total.TotalTokens,
						InputTokens:           delta.InputTokens,
						CachedInputTokens:     delta.CachedInputTokens,
						OutputTokens:          delta.OutputTokens,
						ReasoningOutputTokens: delta.ReasoningOutputTokens,
						TotalTokens:           delta.TotalTokens,
					})
					break
				}
				appendCodexUsageEvent(&events, current, sessionHash, turnID, occurredAt, total.TotalTokens, model, delta, window, seenEventIDs)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, "", fileState, err
		}
	}

	flushPendingCodexUsageEvents(&events, &pendingEvents, current, turnID, model, window, seenEventIDs)
	fileState.Offset = currentOffset
	fileState.SessionID = sessionID
	fileState.CLIVersion = cliVersion
	fileState.TurnID = turnID
	fileState.Model = model
	persistSessionContext(next, sessionID, turnID, model)
	return events, cliVersion, fileState, nil
}

func shouldForceCodexStateRescan(fileState FileState, state *State) bool {
	if fileState.Offset <= 0 {
		return false
	}
	if strings.TrimSpace(fileState.SessionID) == "" {
		return false
	}
	if strings.TrimSpace(fileState.TurnID) == "" && strings.TrimSpace(fileState.Model) == "" {
		return true
	}
	return state == nil || state.SessionContextVersion < reporterStateSessionContextVersion
}

func restoreSessionContext(state *State, sessionID string, turnID string, model string) (string, string) {
	turnID = strings.TrimSpace(turnID)
	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	if state == nil || sessionID == "" {
		return turnID, model
	}
	ctx, ok := state.SessionContexts[sessionID]
	if !ok {
		return turnID, model
	}
	if turnID == "" {
		turnID = strings.TrimSpace(ctx.TurnID)
	}
	if model == "" {
		model = strings.TrimSpace(ctx.Model)
	}
	return turnID, model
}

func persistSessionContext(state *State, sessionID string, turnID string, model string) {
	sessionID = strings.TrimSpace(sessionID)
	if state == nil || sessionID == "" {
		return
	}
	state.ensureMaps()
	state.SessionContexts[sessionID] = SessionContext{
		TurnID: strings.TrimSpace(turnID),
		Model:  strings.TrimSpace(model),
	}
}

func appendCodexUsageEvent(events *[]UsageEvent, current *State, sessionHash string, turnID string, occurredAt int64, runningTotalTokens int, model string, delta TokenUsageTotals, window clientAcceptWindow, seenEventIDs map[string]struct{}) {
	if window.Enabled && (occurredAt < window.CutoffUnix || occurredAt > window.FutureLimit) {
		return
	}
	eventID := sha256Hex(fmt.Sprintf("%s|%s|%d|%d", sessionHash, strings.TrimSpace(turnID), occurredAt, runningTotalTokens))
	if current != nil {
		if _, ok := current.Acknowledged[eventID]; ok {
			return
		}
		if _, ok := current.Rejected[eventID]; ok {
			return
		}
	}
	if seenEventIDs != nil {
		if _, ok := seenEventIDs[eventID]; ok {
			return
		}
		seenEventIDs[eventID] = struct{}{}
	}
	sourceModelName := strings.TrimSpace(model)
	if sourceModelName == "" {
		sourceModelName = "unknown"
	}
	*events = append(*events, UsageEvent{
		EventID:               eventID,
		SessionID:             sessionHash,
		OccurredAt:            occurredAt,
		SourceModelName:       sourceModelName,
		InputTokens:           delta.InputTokens,
		CachedInputTokens:     delta.CachedInputTokens,
		OutputTokens:          delta.OutputTokens,
		ReasoningOutputTokens: delta.ReasoningOutputTokens,
		TotalTokens:           delta.TotalTokens,
	})
}

func flushPendingCodexUsageEvents(events *[]UsageEvent, pending *[]pendingCodexUsageEvent, current *State, turnID string, model string, window clientAcceptWindow, seenEventIDs map[string]struct{}) {
	if len(*pending) == 0 {
		return
	}
	for _, item := range *pending {
		appendCodexUsageEvent(events, current, item.SessionHash, turnID, item.OccurredAt, item.RunningTotalTokens, model, TokenUsageTotals{
			InputTokens:           item.InputTokens,
			CachedInputTokens:     item.CachedInputTokens,
			OutputTokens:          item.OutputTokens,
			ReasoningOutputTokens: item.ReasoningOutputTokens,
			TotalTokens:           item.TotalTokens,
		}, window, seenEventIDs)
	}
	*pending = (*pending)[:0]
}

func listCodexUsageFiles(root string) ([]string, error) {
	files := make([]string, 0)
	seen := make(map[string]struct{})

	if err := appendJSONLFiles(root, &files, seen); err != nil {
		return nil, err
	}
	if filepath.Base(filepath.Clean(root)) == "sessions" {
		archivedRoot := filepath.Join(filepath.Dir(filepath.Clean(root)), "archived_sessions")
		if err := appendJSONLFiles(archivedRoot, &files, seen); err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func appendJSONLFiles(root string, files *[]string, seen map[string]struct{}) error {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			if _, ok := seen[path]; !ok {
				*files = append(*files, path)
				seen[path] = struct{}{}
			}
		}
		return nil
	})
}

func diffTotals(prev TokenUsageTotals, current TokenUsageTotals) TokenUsageTotals {
	delta := TokenUsageTotals{
		InputTokens:           current.InputTokens - prev.InputTokens,
		CachedInputTokens:     current.CachedInputTokens - prev.CachedInputTokens,
		OutputTokens:          current.OutputTokens - prev.OutputTokens,
		ReasoningOutputTokens: current.ReasoningOutputTokens - prev.ReasoningOutputTokens,
		TotalTokens:           current.TotalTokens - prev.TotalTokens,
	}
	if delta.InputTokens < 0 {
		delta.InputTokens = 0
	}
	if delta.CachedInputTokens < 0 {
		delta.CachedInputTokens = 0
	}
	if delta.OutputTokens < 0 {
		delta.OutputTokens = 0
	}
	if delta.ReasoningOutputTokens < 0 {
		delta.ReasoningOutputTokens = 0
	}
	if delta.TotalTokens < 0 {
		delta.TotalTokens = 0
	}
	return delta
}

func mergeSessionTotals(prev TokenUsageTotals, current TokenUsageTotals) TokenUsageTotals {
	merged := prev
	if current.InputTokens > merged.InputTokens {
		merged.InputTokens = current.InputTokens
	}
	if current.CachedInputTokens > merged.CachedInputTokens {
		merged.CachedInputTokens = current.CachedInputTokens
	}
	if current.OutputTokens > merged.OutputTokens {
		merged.OutputTokens = current.OutputTokens
	}
	if current.ReasoningOutputTokens > merged.ReasoningOutputTokens {
		merged.ReasoningOutputTokens = current.ReasoningOutputTokens
	}
	if current.TotalTokens > merged.TotalTokens {
		merged.TotalTokens = current.TotalTokens
	}
	return merged
}

func parseOccurredAt(value string) int64 {
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return ts.Unix()
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
