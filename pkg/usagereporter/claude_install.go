package usagereporter

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// InstallClaudeReporter exchanges a source-bound one-time token and prepares
// private configuration. Service registration is done by the platform script.
func InstallClaudeReporter(server, token, path, settingsPath string) error {
	if _, err := NormalizeClaudeUpstreamURL(server); err != nil || strings.TrimSpace(token) == "" {
		return errors.New("a valid server and install token are required")
	}
	previous, err := readConfig(path)
	if err != nil && !os.IsNotExist(err) {
		return errors.New("could not read existing reporter configuration")
	}
	if err == nil {
		if previous.Source != SourceClaudeCode {
			return errors.New("refusing to overwrite a different source's reporter configuration")
		}
		if serverIdentity(previous.Server) != serverIdentity(server) {
			return errors.New("existing installation belongs to a different gateway")
		}
		previous = previous.resolveRelativePaths(filepath.Dir(path))
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	existingCredential := ""
	if serverIdentity(previous.Server) == serverIdentity(server) {
		existingCredential = previous.Credential
	}
	fingerprint := sha256Hex(SourceClaudeCode + "|" + runtime.GOOS + "-" + runtime.GOARCH + "|" + hostname + "|" + home)
	body, err := common.Marshal(map[string]string{"install_token": token, "device_name": hostname, "device_fingerprint": fingerprint, "reporting_credential": existingCredential})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(server, "/")+"/api/external-usage/self/claude-code-installer/exchange", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("could not contact the Claude installer exchange")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("Claude installer exchange failed")
	}
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Source            string `json:"source"`
			Credential        string `json:"reporting_credential"`
			UpstreamURL       string `json:"upstream_url"`
			UpstreamIsGateway bool   `json:"upstream_is_gateway"`
			AcceptWindowDays  int    `json:"accept_window_days"`
		} `json:"data"`
	}
	if err := common.DecodeJson(resp.Body, &response); err != nil {
		return errors.New("invalid Claude installer exchange response")
	}
	if !response.Success || response.Data.Source != SourceClaudeCode || response.Data.Credential == "" {
		return errors.New("Claude installer exchange rejected; check token expiry, source enablement and device limit")
	}
	cfg := previous
	cfg.Server = server
	cfg.Source = SourceClaudeCode
	cfg.Credential = response.Data.Credential
	cfg.AcceptWindowDays = response.Data.AcceptWindowDays
	if cfg.StatePath == "" {
		cfg.StatePath = filepath.Join(filepath.Dir(path), "state.json")
	}
	if cfg.ClaudeReceiverToken == "" {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		cfg.ClaudeReceiverToken = hex.EncodeToString(random[:])
	}
	cfg.ClaudeAutoRoute = true
	// A legacy, explicitly declared gateway alias remains excluded on upgrade.
	if response.Data.UpstreamIsGateway && response.Data.UpstreamURL != "" {
		upstream, err := NormalizeClaudeUpstreamURL(response.Data.UpstreamURL)
		if err != nil {
			return errors.New("installer exchange returned an invalid gateway route")
		}
		cfg.GatewayURLs = append(cfg.GatewayURLs, upstream)
	}
	if err := SaveConfig(path, cfg); err != nil {
		return err
	}
	binaryName := "usage-reporter"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	if err := ConfigureClaudeTelemetry(cfg, settingsPath, filepath.Join(filepath.Dir(path), binaryName)); err != nil {
		return err
	}
	return nil
}
