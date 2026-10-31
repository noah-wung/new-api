package usagereporter

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/QuantumNous/new-api/common"
)

const zcodeSnapshotLimit = 10000

type zcodeSnapshot struct {
	Event      UsageEvent     `json:"event"`
	ProviderID string         `json:"provider_id"`
	StartedAt  int64          `json:"started_at"`
	Held       bool           `json:"held"`
	Submitted  bool           `json:"submitted"`
	Delivery   string         `json:"delivery,omitempty"`
	Conflict   *zcodeConflict `json:"conflict,omitempty"`
}

type zcodeConflict struct {
	Event      UsageEvent `json:"event"`
	ProviderID string     `json:"provider_id"`
	StartedAt  int64      `json:"started_at"`
}

type zcodeState struct {
	Version      int                      `json:"version"`
	Namespace    string                   `json:"namespace"`
	Snapshots    map[string]zcodeSnapshot `json:"snapshots"`
	InvalidCount int                      `json:"invalid_count"`
	Invalid      map[string]zcodeInvalid  `json:"invalid,omitempty"`
}

type zcodeInvalid struct {
	Input, Output, Reasoning, CacheWrite, CacheRead, Total int64
	InvalidNumeric                                         bool `json:"invalid_numeric,omitempty"`
}

func loadZCodeState(path, namespace string) (*zcodeState, error) {
	state := &zcodeState{Version: 1, Namespace: namespace, Snapshots: make(map[string]zcodeSnapshot), Invalid: make(map[string]zcodeInvalid)}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := common.Unmarshal(data, state); err != nil || state.Version != 1 || state.Snapshots == nil {
		return nil, errors.New("invalid ZCode state")
	}
	if state.Namespace != namespace {
		return nil, errors.New("ZCode credential or server changed; preserve and migrate the existing queue and state explicitly")
	}
	if state.Invalid == nil {
		state.Invalid = make(map[string]zcodeInvalid)
	}
	return state, nil
}

func (s *zcodeState) observe(event UsageEvent, provider string, startedAt int64) error {
	old, exists := s.Snapshots[event.EventID]
	if !exists {
		if len(s.Snapshots)+len(s.Invalid) >= zcodeSnapshotLimit {
			return errors.New("ZCode snapshot capacity reached")
		}
		s.Snapshots[event.EventID] = zcodeSnapshot{Event: event, ProviderID: provider, StartedAt: startedAt}
		return nil
	}
	if old.Event != event || old.ProviderID != provider || old.StartedAt != startedAt {
		if old.Held {
			if old.Conflict == nil {
				old.Conflict = &zcodeConflict{Event: event, ProviderID: provider, StartedAt: startedAt}
			}
		} else {
			old.Event, old.ProviderID, old.StartedAt = event, provider, startedAt
		}
		s.Snapshots[event.EventID] = old
	}
	return nil
}

func (s *zcodeState) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := common.Marshal(s)
	if err != nil {
		return err
	}
	return writePrivateFile(path, data)
}
