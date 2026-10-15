package reporterupdate

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoticeChecksDailyAndSharesNotificationButNotSourceCredentials(t *testing.T) {
	now := time.Unix(1800000000, 0)
	checks, notifications := 0, 0
	statuses := map[string]int{}
	available := "2.0.0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/external-usage/reporter/version":
			checks++
			assert.Empty(t, r.Header.Get("Authorization"))
			assert.Equal(t, "windows-amd64", r.URL.Query().Get("target"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"reporter_version":"` + available + `","command":"must never execute","download_url":"http://untrusted.invalid/program"}}`))
		case "/api/external-usage/reporter/status":
			var status struct {
				Source, Phase string
				Version       string `json:"reporter_version"`
				Auto          bool   `json:"auto_update"`
			}
			require.NoError(t, common.DecodeJson(r.Body, &status))
			assert.Equal(t, "Bearer "+status.Source, r.Header.Get("Authorization"))
			assert.False(t, status.Auto)
			assert.Equal(t, "update_available", status.Phase)
			assert.Equal(t, Version, status.Version)
			statuses[status.Source]++
			if status.Source == "codex" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			t.Errorf("unexpected request, notification checks must never download assets: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	n := NoticeChecker{Root: t.TempDir(), Server: server.URL, Target: "windows-amd64", HTTP: server.Client(), Now: func() time.Time { return now }, Notify: func(_ context.Context, version, page string) error {
		notifications++
		assert.Equal(t, available, version)
		assert.Equal(t, server.URL+"/external-usage", page)
		return nil
	}}
	var output bytes.Buffer
	n.Source, n.Credential = "codex", "codex"
	require.NoError(t, n.Check(context.Background(), &output))
	n.Source, n.Credential = "claude_code", "claude_code"
	require.NoError(t, n.Check(context.Background(), &output))
	require.NoError(t, n.Check(context.Background(), &output))
	assert.Equal(t, 1, checks)
	assert.Equal(t, 1, notifications)
	assert.Equal(t, 1, statuses["claude_code"], "revoked Codex status must not block Claude or duplicate its successful receipt")
	assert.Contains(t, output.String(), "generate a new installation command")
	assert.NotContains(t, output.String(), "untrusted.invalid")
	now = now.Add(24 * time.Hour)
	require.NoError(t, n.Check(context.Background(), &output))
	assert.Equal(t, 2, checks)
	assert.Equal(t, 1, notifications, "the same release is notified only once")
	available = "2.1.0"
	now = now.Add(24 * time.Hour)
	require.NoError(t, n.Check(context.Background(), &output))
	assert.Equal(t, 2, notifications, "a later release gets its own notification")
}

func TestNoticeRejectsInvalidMetadataAndBacksOffWithoutPrompting(t *testing.T) {
	for _, body := range []string{
		`{"success":true,"data":{"reporter_version":"2.0.0; launch malware"}}`,
		`{"success":false,"data":{"reporter_version":"2.0.0"}}`,
		strings.Repeat("x", 5000),
	} {
		t.Run(body[:12], func(t *testing.T) {
			checks := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { checks++; _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			now := time.Unix(1800000000, 0)
			n := NoticeChecker{Root: t.TempDir(), Server: server.URL, Target: "darwin-arm64", Now: func() time.Time { return now }, Notify: func(context.Context, string, string) error { t.Error("invalid metadata must not notify"); return nil }}
			require.EqualError(t, n.Check(context.Background(), nil), "release_unavailable")
			require.EqualError(t, n.Check(context.Background(), nil), "release_unavailable")
			assert.Equal(t, 1, checks)
			now = now.Add(time.Hour)
			require.Error(t, n.Check(context.Background(), nil))
			assert.Equal(t, 2, checks)
		})
	}
}

func TestNoticeDoesNotPromptForCurrentOrOlderVersion(t *testing.T) {
	for _, version := range []string{Version, "0.1.0"} {
		t.Run(version, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"success":true,"data":{"reporter_version":"` + version + `"}}`))
			}))
			defer server.Close()
			n := NoticeChecker{Root: t.TempDir(), Server: server.URL, Target: "darwin-amd64", Notify: func(context.Context, string, string) error { t.Error("must not notify"); return nil }}
			require.NoError(t, n.Check(context.Background(), nil))
		})
	}
}

func TestNoticeNotificationFailureDoesNotRepeatEveryCollectorRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"reporter_version":"2.0.0"}}`))
	}))
	defer server.Close()
	calls := 0
	n := NoticeChecker{Root: t.TempDir(), Server: server.URL, Target: "darwin-arm64", Notify: func(context.Context, string, string) error { calls++; return errors.New("notifications disabled") }}
	require.NoError(t, n.Check(context.Background(), nil))
	require.NoError(t, n.Check(context.Background(), nil))
	assert.Equal(t, 1, calls)
}

func TestNoticeSharesDailyCheckAcrossDifferentInstalledVersions(t *testing.T) {
	previous := Version
	t.Cleanup(func() { Version = previous })
	checks, notifications := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks++
		_, _ = w.Write([]byte(`{"success":true,"data":{"reporter_version":"2.0.0"}}`))
	}))
	defer server.Close()
	n := NoticeChecker{Root: t.TempDir(), Server: server.URL, Target: "darwin-arm64", Notify: func(context.Context, string, string) error { notifications++; return nil }}
	Version = "1.0.1"
	require.NoError(t, n.Check(context.Background(), nil))
	Version = "2.0.0"
	require.NoError(t, n.Check(context.Background(), nil))
	Version = "1.0.1"
	require.NoError(t, n.Check(context.Background(), nil))
	assert.Equal(t, 1, checks)
	assert.Equal(t, 1, notifications)
}
