package usagereporter

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type publicStatusResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ExternalUsageAcceptWindowDays int `json:"external_usage_accept_window_days"`
	} `json:"data"`
}

func BackfillAcceptWindowDaysFromServer(client *http.Client, cfg *Config) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	if cfg.AcceptWindowDays > 0 {
		return false, nil
	}
	if strings.TrimSpace(cfg.Source) != "" && strings.TrimSpace(cfg.Source) != SourceCodex {
		return false, nil
	}
	serverURL := strings.TrimRight(strings.TrimSpace(cfg.Server), "/")
	if serverURL == "" {
		return false, fmt.Errorf("server is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, serverURL+"/api/status", nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("unexpected status response: %d", resp.StatusCode)
	}
	var decoded publicStatusResponse
	if err := common.DecodeJson(resp.Body, &decoded); err != nil {
		return false, err
	}
	if decoded.Data.ExternalUsageAcceptWindowDays <= 0 {
		return false, fmt.Errorf("external_usage_accept_window_days is missing")
	}
	cfg.AcceptWindowDays = decoded.Data.ExternalUsageAcceptWindowDays
	return true, nil
}
