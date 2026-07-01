package usagereporter

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBackfillAcceptWindowDaysFromServer_UpdatesMissingValue(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet {
				t.Fatalf("r.Method = %q, want GET", r.Method)
			}
			if r.URL.String() != "https://example.test/api/status" {
				t.Fatalf("r.URL.String() = %q, want %q", r.URL.String(), "https://example.test/api/status")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"success":true,"data":{"external_usage_accept_window_days":90}}`)),
			}, nil
		}),
	}

	cfg := Config{
		Server:           "https://example.test",
		Source:           SourceCodex,
		Credential:       "cred-1",
		AcceptWindowDays: 0,
	}
	changed, err := BackfillAcceptWindowDaysFromServer(client, &cfg)
	if err != nil {
		t.Fatalf("BackfillAcceptWindowDaysFromServer error: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if cfg.AcceptWindowDays != 90 {
		t.Fatalf("cfg.AcceptWindowDays = %d, want 90", cfg.AcceptWindowDays)
	}
}

func TestBackfillAcceptWindowDaysFromServer_SkipsWhenAlreadyPresent(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("unexpected HTTP request")
			return nil, nil
		}),
	}

	cfg := Config{
		Server:           "https://example.test",
		Source:           SourceCodex,
		Credential:       "cred-1",
		AcceptWindowDays: 90,
	}
	changed, err := BackfillAcceptWindowDaysFromServer(client, &cfg)
	if err != nil {
		t.Fatalf("BackfillAcceptWindowDaysFromServer error: %v", err)
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
}

func TestBackfillAcceptWindowDaysFromServer_RejectsMissingServerValue(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"success":true,"data":{"external_usage_accept_window_days":0}}`)),
			}, nil
		}),
	}

	cfg := Config{
		Server:           "https://example.test",
		Source:           SourceCodex,
		Credential:       "cred-1",
		AcceptWindowDays: 0,
	}
	changed, err := BackfillAcceptWindowDaysFromServer(client, &cfg)
	if err == nil {
		t.Fatal("BackfillAcceptWindowDaysFromServer error = nil, want non-nil")
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
}
