package usagereporter

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/QuantumNous/new-api/common"
)

const zcodeSnapshotLimit = 10000
const zcodeArchiveThreshold = 9000

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
	Version         int                      `json:"version"`
	Namespace       string                   `json:"namespace"`
	Snapshots       map[string]zcodeSnapshot `json:"snapshots"`
	InvalidCount    int                      `json:"invalid_count"`
	Invalid         map[string]zcodeInvalid  `json:"invalid,omitempty"`
	ArchiveRequired bool                     `json:"archive_required,omitempty"`
	ArchiveCount    int64                    `json:"archive_count,omitempty"`
	archive         *zcodeArchive
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
	if err := common.Unmarshal(data, state); err != nil || (state.Version != 1 && state.Version != 2) || state.Snapshots == nil || (state.Version == 2) != state.ArchiveRequired || state.ArchiveRequired && state.ArchiveCount < 1 {
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
	if !exists && s.archive != nil {
		archived, found, err := s.archive.get(event.EventID)
		if err != nil {
			return err
		}
		if found {
			if archived.Event != event || archived.ProviderID != provider || archived.StartedAt != startedAt {
				if len(s.Snapshots)+len(s.Invalid) >= zcodeSnapshotLimit {
					return errors.New("ZCode snapshot capacity reached")
				}
				archived.Conflict = &zcodeConflict{Event: event, ProviderID: provider, StartedAt: startedAt}
				s.Snapshots[event.EventID] = archived
			}
			return nil
		}
	}
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

func (s *zcodeState) validateArchive(archive *zcodeArchive) error {
	if !s.ArchiveRequired {
		return nil
	}
	if archive == nil {
		return errors.New("required ZCode archive is missing")
	}
	count, err := archive.count()
	if err != nil {
		return err
	}
	if count < s.ArchiveCount {
		return errors.New("ZCode archive is incomplete")
	}
	return nil
}

// Archive only acknowledged, conflict-free records after their queue files have
// been removed. Commit the archive first; a crash before the JSON checkpoint
// then leaves two identical copies, never an unrecorded acknowledgement.
func (s *zcodeState) compact(path string) error {
	if len(s.Snapshots)+len(s.Invalid) < zcodeArchiveThreshold {
		return nil
	}
	eligible := make(map[string]zcodeSnapshot)
	for id, snapshot := range s.Snapshots {
		if snapshot.Held && snapshot.Submitted && snapshot.Conflict == nil && (snapshot.Delivery == "accepted" || snapshot.Delivery == "duplicate") {
			eligible[id] = snapshot
		}
	}
	if len(eligible) == 0 {
		return nil
	}
	if s.archive == nil {
		var err error
		s.archive, err = openZCodeArchive(path, s.Namespace, true, false)
		if err != nil {
			return err
		}
	}
	if err := s.archive.store(eligible); err != nil {
		return err
	}
	archivedCount, err := s.archive.count()
	if err != nil {
		return err
	}
	for id := range eligible {
		delete(s.Snapshots, id)
	}
	oldVersion, oldRequired, oldCount := s.Version, s.ArchiveRequired, s.ArchiveCount
	s.Version, s.ArchiveRequired, s.ArchiveCount = 2, true, archivedCount
	if err := s.save(path); err != nil {
		s.Version, s.ArchiveRequired, s.ArchiveCount = oldVersion, oldRequired, oldCount
		for id, snapshot := range eligible {
			s.Snapshots[id] = snapshot
		}
		return err
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
