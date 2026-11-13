package usagereporter

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// InstallZCodeReporter preserves a valid ZCode installation's identity and all local
// paths. Switching identities is deliberately not a reinstall operation.
func InstallZCodeReporter(server, token, path string) error {
	previous, e := readConfig(path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if e == nil && (previous.Source != SourceZCode || serverIdentity(previous.Server) != serverIdentity(server)) {
		return errors.New("existing installation belongs to a different identity")
	}
	if e == nil {
		previous = previous.resolveRelativePaths(filepath.Dir(path))
	}
	// A first-time ZCode install has no previous config. Validate local data
	// against the requested server without changing the preserved config.
	preflight := previous
	preflight.Server = server
	preflight.Source = SourceZCode
	if err := checkZCodeLocalData(preflight.WithDefaults()); err != nil {
		return err
	}
	host, e := os.Hostname()
	if e != nil {
		return e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	fingerprint := sha256Hex(SourceZCode + "|" + runtime.GOOS + "-" + runtime.GOARCH + "|" + host + "|" + home)
	body, e := common.Marshal(map[string]string{"install_token": token, "device_name": host, "device_fingerprint": fingerprint, "reporting_credential": previous.Credential})
	if e != nil {
		return e
	}
	req, e := http.NewRequest(http.MethodPost, strings.TrimRight(server, "/")+"/api/external-usage/self/zcode-installer/exchange", bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return errors.New("could not contact installer exchange")
	}
	defer resp.Body.Close()
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Credential       string `json:"reporting_credential"`
			Source           string `json:"source"`
			AcceptWindowDays int    `json:"accept_window_days"`
		} `json:"data"`
	}
	if resp.StatusCode != 200 || common.DecodeJson(resp.Body, &response) != nil || !response.Success || response.Data.Credential == "" || response.Data.Source != SourceZCode {
		return errors.New("installer exchange rejected; existing identity was not replaced")
	}
	cfg := previous
	cfg.Server = server
	cfg.Source = SourceZCode
	cfg.Credential = response.Data.Credential
	if cfg.StatePath == "" {
		cfg.StatePath = filepath.Join(filepath.Dir(path), "state.json")
	}
	if response.Data.AcceptWindowDays > 0 {
		cfg.AcceptWindowDays = response.Data.AcceptWindowDays
	}
	return SaveConfig(path, cfg)
}
