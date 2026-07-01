package usagereporter

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type TokenUsageTotals struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

const reporterStateSessionContextVersion = 1

type FileState struct {
	Offset     int64  `json:"offset"`
	SessionID  string `json:"session_id"`
	CLIVersion string `json:"cli_version"`
	TurnID     string `json:"turn_id,omitempty"`
	Model      string `json:"model,omitempty"`
}

type SessionContext struct {
	TurnID string `json:"turn_id,omitempty"`
	Model  string `json:"model,omitempty"`
}

type State struct {
	Files                 map[string]FileState        `json:"files"`
	SessionTotals         map[string]TokenUsageTotals `json:"session_totals"`
	SessionContexts       map[string]SessionContext   `json:"session_contexts,omitempty"`
	SessionContextVersion int                         `json:"session_context_version,omitempty"`
	Acknowledged          map[string]int64            `json:"acknowledged"`
	Rejected              map[string]string           `json:"rejected"`
	Server                string                      `json:"server,omitempty"`
}

func NewState() *State {
	return &State{
		Files:                 make(map[string]FileState),
		SessionTotals:         make(map[string]TokenUsageTotals),
		SessionContexts:       make(map[string]SessionContext),
		SessionContextVersion: reporterStateSessionContextVersion,
		Acknowledged:          make(map[string]int64),
		Rejected:              make(map[string]string),
	}
}

func LoadState(path string) (*State, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewState(), nil
		}
		return nil, err
	}
	state := NewState()
	if len(content) == 0 {
		return state, nil
	}
	if err := common.Unmarshal(content, state); err != nil {
		return nil, err
	}
	state.ensureMaps()
	return state, nil
}

func (s *State) Save(path string) error {
	s.ensureMaps()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content, err := common.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func (s *State) Clone() *State {
	if s == nil {
		return NewState()
	}
	clone := NewState()
	clone.Server = s.Server
	clone.SessionContextVersion = s.SessionContextVersion
	for key, value := range s.Files {
		clone.Files[key] = value
	}
	for key, value := range s.SessionTotals {
		clone.SessionTotals[key] = value
	}
	for key, value := range s.SessionContexts {
		clone.SessionContexts[key] = value
	}
	for key, value := range s.Acknowledged {
		clone.Acknowledged[key] = value
	}
	for key, value := range s.Rejected {
		clone.Rejected[key] = value
	}
	return clone
}

func (s *State) PrepareForServer(server string) *State {
	if s == nil {
		return NewState()
	}
	if s.Server == "" || serverIdentity(s.Server) == serverIdentity(server) {
		return s
	}
	next := NewState()
	next.Server = strings.TrimSpace(server)
	return next
}

func ApplyAcknowledgements(current *State, next *State, ack *ReportAcknowledgement) {
	if current == nil || next == nil || ack == nil {
		return
	}
	cloned := next.Clone()
	current.Files = cloned.Files
	current.SessionTotals = cloned.SessionTotals
	current.SessionContexts = cloned.SessionContexts
	current.SessionContextVersion = cloned.SessionContextVersion
	now := time.Now().Unix()
	current.ensureMaps()
	for _, eventID := range ack.AcceptedEventIDs {
		current.Acknowledged[eventID] = now
		delete(current.Rejected, eventID)
	}
	for _, eventID := range ack.DuplicateEventIDs {
		current.Acknowledged[eventID] = now
		delete(current.Rejected, eventID)
	}
	for _, item := range ack.Rejected {
		current.Rejected[item.EventID] = item.Reason
	}
}

func (s *State) ensureMaps() {
	if s.Files == nil {
		s.Files = make(map[string]FileState)
	}
	if s.SessionTotals == nil {
		s.SessionTotals = make(map[string]TokenUsageTotals)
	}
	if s.SessionContexts == nil {
		s.SessionContexts = make(map[string]SessionContext)
	}
	if s.Acknowledged == nil {
		s.Acknowledged = make(map[string]int64)
	}
	if s.Rejected == nil {
		s.Rejected = make(map[string]string)
	}
}
