package reporterupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// NoticeInfo is advisory only. It deliberately contains no executable URL,
// command, signature or installation instruction supplied by the server.
type NoticeInfo struct {
	Version string `json:"reporter_version"`
}

type noticeState struct {
	Available   string            `json:"available_version"`
	LastCheck   int64             `json:"last_check_at"`
	NextCheck   int64             `json:"next_check_at"`
	Phase       string            `json:"phase"`
	Error       string            `json:"error_code"`
	Notified    string            `json:"notified_version"`
	NotifyAfter int64             `json:"notify_after"`
	Receipts    map[string]string `json:"receipts"`
}

// NoticeChecker shares check/notification state between all client adapters.
// Its only writes are local metadata, OS notifications and device status.
type NoticeChecker struct {
	Root, Server, Source, Credential, Target string
	HTTP                                     *http.Client
	Notify                                   func(context.Context, string, string) error
	Now                                      func() time.Time
}

func (n *NoticeChecker) Check(ctx context.Context, output io.Writer) error {
	origin, err := Origin(n.Server)
	if err != nil {
		return err
	}
	if _, err = TargetName(n.Target); err != nil {
		return err
	}
	if err = SecureDir(n.Root); err != nil {
		return err
	}
	release, err := Lock(filepath.Join(n.Root, "notice.lock"))
	if err != nil {
		return nil
	} // Another collector owns the shared check.
	defer release()
	path := filepath.Join(n.Root, Digest([]byte(origin+"\x00"+n.Target))+".json")
	var state noticeState
	if err = Load(path, &state); err != nil && !os.IsNotExist(err) {
		return err
	}
	now := time.Now()
	if n.Now != nil {
		now = n.Now()
	}
	client := n.HTTP
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if now.Unix() >= state.NextCheck {
		state.LastCheck = now.Unix()
		state.NextCheck = now.Add(time.Hour).Unix()
		state.Phase, state.Error = "check_failed", "release_unavailable"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/api/external-usage/reporter/version?target="+url.QueryEscape(n.Target)+"&source="+url.QueryEscape(n.Source), nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			state.Error = "network_error"
		} else {
			var response struct {
				Success bool       `json:"success"`
				Data    NoticeInfo `json:"data"`
			}
			decodeErr := common.DecodeJson(io.LimitReader(resp.Body, 4096), &response)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decodeErr == nil && response.Success && ValidVersion(response.Data.Version) {
				state.Available = response.Data.Version
				state.Phase, state.Error = "notice_current", ""
				state.NextCheck = now.Add(24 * time.Hour).Unix()
			}
		}
	}
	if output == nil {
		output = io.Discard
	}
	// The metadata cache is shared even when selected clients are running
	// different local versions during a partial manual upgrade.
	available := ""
	phase := state.Phase
	if Newer(state.Available, Version) {
		available = state.Available
		if state.Error == "" {
			phase = "update_available"
		}
	}
	if phase == "update_available" && state.Notified != available && now.Unix() >= state.NotifyAfter {
		state.NotifyAfter = now.Add(24 * time.Hour).Unix()
		page := origin + "/external-usage"
		fmt.Fprintf(output, "Collector update available: %s (installed %s). Open %s and generate a new installation command to upgrade manually.\n", state.Available, Version, page)
		notify := n.Notify
		if notify == nil {
			notify = NotifyUpgrade
		}
		if err := notify(ctx, state.Available, page); err == nil {
			state.Notified = state.Available
		} else {
			fmt.Fprintln(output, "Collector update notification unavailable; check the personal External Usage page.")
		}
	}
	if state.Receipts == nil {
		state.Receipts = map[string]string{}
	}
	// Reuse each source's existing reporting identity. HTTP follows the gateway's
	// existing report transport; no credentials go to the public version endpoint.
	body, err := common.Marshal(struct {
		Source    string `json:"source"`
		Version   string `json:"reporter_version"`
		Available string `json:"available_version"`
		Auto      bool   `json:"auto_update"`
		Phase     string `json:"phase"`
		Error     string `json:"error_code"`
		LastCheck int64  `json:"last_check_at"`
	}{n.Source, Version, available, false, phase, state.Error, state.LastCheck})
	if err != nil {
		return err
	}
	identity := Digest([]byte(n.Source + "\x00" + n.Credential))
	fingerprint := Digest(body)
	if n.Credential != "" && state.Receipts[identity] != fingerprint {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/external-usage/reporter/status", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+n.Credential)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			var result struct {
				Success bool `json:"success"`
			}
			decodeErr := common.DecodeJson(io.LimitReader(resp.Body, 4096), &result)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decodeErr == nil && result.Success {
				state.Receipts[identity] = fingerprint
			}
		}
	}
	if err = Save(path, state); err != nil {
		return err
	}
	if state.Error != "" {
		return errors.New(state.Error)
	}
	return nil
}
