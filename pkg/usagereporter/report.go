package usagereporter

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type ReportPayload struct {
	Source        string       `json:"source"`
	ClientVersion string       `json:"client_version"`
	Events        []UsageEvent `json:"events"`
}

type RejectedEvent struct {
	EventID string `json:"event_id"`
	Reason  string `json:"reason"`
}

type ReportAcknowledgement struct {
	AcceptedEventIDs  []string        `json:"accepted_event_ids"`
	DuplicateEventIDs []string        `json:"duplicate_event_ids"`
	Rejected          []RejectedEvent `json:"rejected"`
}

type reportResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    ReportAcknowledgement `json:"data"`
}

func UploadReport(serverURL string, credential string, payload ReportPayload) (*ReportAcknowledgement, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	return uploadReportWithClient(client, serverURL, credential, payload)
}

func uploadReportWithClient(client *http.Client, serverURL string, credential string, payload ReportPayload) (*ReportAcknowledgement, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(serverURL), "/") + "/api/external-usage/report"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(credential))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected report status: %d", resp.StatusCode)
	}

	var decoded reportResponse
	if err := common.DecodeJson(resp.Body, &decoded); err != nil {
		return nil, err
	}
	if !decoded.Success {
		if strings.TrimSpace(decoded.Message) == "" {
			return nil, errors.New("external usage report failed")
		}
		return nil, errors.New(decoded.Message)
	}
	return &decoded.Data, nil
}
