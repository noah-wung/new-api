package reporterupdate

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

func ValidPhase(v string) bool {
	switch v {
	case "notice_current", "update_available", "idle", "checking", "downloading", "prepared", "switching", "health_check", "committed", "check_failed", "rolling_back", "rolled_back", "recovery_required":
		return true
	}
	return false
}
func ValidErrorCode(v string) bool {
	switch v {
	case "", "recovery_required", "release_withdrawn", "network_error", "release_unavailable", "invalid_signature", "untrusted_key", "expired_catalog", "catalog_replay", "artifact_mismatch", "incompatible_release", "self_check_failed", "insufficient_space", "recovery_pending", "health_check_failed", "stop_failed", "task_switch_failed", "key_conflict", "update_failed":
		return true
	}
	return false
}

// ErrorCode prevents arbitrary server responses, paths and credentials from
// becoming persisted diagnostics or background log messages.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	reason := err.Error()
	if reason != "" && ValidErrorCode(reason) {
		return reason
	}
	return "update_failed"
}

// SendStatus uses each profile's existing credential and never sends it over HTTP.
func (m *Manager) SendStatus(ctx context.Context) error {
	i, s, e := m.read()
	if e != nil {
		return e
	}
	if !strings.HasPrefix(i.Server, "https://") {
		return nil
	}
	if !ValidPhase(s.Phase) {
		s.Phase = "idle"
	}

	fingerprintData, e := common.Marshal(struct {
		Version, Available, Phase, Error string
		Auto                             bool
		Check                            int64
	}{i.Current.Version, s.Available, s.Phase, s.Error, i.AutoUpdate, s.LastCheck})
	if e != nil {
		return e
	}
	fingerprint := Digest(fingerprintData)
	type receipt struct {
		At   int64  `json:"at"`
		Hash string `json:"hash"`
	}
	sent := map[string]receipt{}
	if e = Load(filepath.Join(m.Root, "status-sent.json"), &sent); e != nil && !os.IsNotExist(e) {
		return e
	}
	if sent == nil {
		sent = map[string]receipt{}
	}
	var firstErr error
	for _, p := range i.Profiles {
		var cfg struct {
			Server     string `json:"server"`
			Credential string `json:"credential"`
			Source     string `json:"source"`
		}
		if e = Load(p.Config, &cfg); e != nil {
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		origin, e := Origin(cfg.Server)
		if e != nil || origin != i.Server || cfg.Source != p.Source {
			if firstErr == nil {
				firstErr = os.ErrPermission
			}
			continue
		}
		identity := Digest([]byte(origin + "\x00" + cfg.Credential + "\x00" + cfg.Source))
		if last := sent[identity]; last.Hash == fingerprint && last.At > m.now().Add(-24*time.Hour).Unix() {
			continue
		}
		body, e := common.Marshal(struct {
			Source    string `json:"source"`
			Version   string `json:"reporter_version"`
			Available string `json:"available_version"`
			Auto      bool   `json:"auto_update"`
			Phase     string `json:"phase"`
			Error     string `json:"error_code"`
			LastCheck int64  `json:"last_check_at"`
		}{p.Source, i.Current.Version, s.Available, i.AutoUpdate, s.Phase, s.Error, s.LastCheck})
		if e != nil {
			return e
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, i.Server+"/api/external-usage/reporter/status", bytes.NewReader(body))
		if e != nil {
			return e
		}
		req.Header.Set("Authorization", "Bearer "+cfg.Credential)
		req.Header.Set("Content-Type", "application/json")
		resp, e := m.HTTP.Do(req)
		if e != nil {
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		var result struct {
			Success bool `json:"success"`
		}
		decodeErr := common.DecodeJson(io.LimitReader(resp.Body, 4096), &result)
		resp.Body.Close()
		if resp.StatusCode != 200 || decodeErr != nil || !result.Success {
			if firstErr == nil {
				firstErr = os.ErrPermission
			}
			continue
		}
		sent[identity] = receipt{At: m.now().Unix(), Hash: fingerprint}
	}
	if e = Save(filepath.Join(m.Root, "status-sent.json"), sent); e != nil {
		return e
	}
	return firstErr
}
