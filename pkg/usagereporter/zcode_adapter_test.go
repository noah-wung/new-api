package usagereporter

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestZCodeUnresolvedSurvivesSourceCleanupAndDetectsRevision(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER);`)
	require.NoError(t, err)
	now := time.Now()
	_, err = db.Exec(`INSERT INTO model_usage VALUES ('id','session','provider','model','completed',?,?,10,5,2,0,1,15)`, now.UnixMilli(), now.UnixMilli())
	require.NoError(t, err)
	var uploads []ReportPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		uploads = append(uploads, payload)
		var ids []string
		for _, event := range payload.Events {
			ids = append(ids, event.EventID)
		}
		wire, err := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		require.NoError(t, err)
		_, err = w.Write(wire)
		require.NoError(t, err)
	}))
	defer server.Close()
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath, AcceptWindowDays: 90}
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Empty(t, uploads)
	_, err = db.Exec(`DELETE FROM model_usage`)
	require.NoError(t, err)
	cfg.ZCodeProviderRoutes = []ZCodeProviderRoute{{ProviderID: "provider", Origin: "https://external.example", From: now.Add(-time.Hour).Unix(), Until: now.Add(time.Hour).Unix()}}
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Len(t, uploads, 1)
	wire, err := common.Marshal(uploads[0])
	require.NoError(t, err)
	require.NotContains(t, string(wire), "provider")
	require.NotContains(t, string(wire), "external.example")
	_, err = db.Exec(`INSERT INTO model_usage VALUES ('id','session','provider','model','completed',?,?,11,5,2,0,1,16)`, now.UnixMilli(), now.UnixMilli())
	require.NoError(t, err)
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Len(t, uploads, 1)
	state, err := loadZCodeState(cfg.StatePath, sha256Hex(serverIdentity(cfg.Server)+"\x00"+cfg.Credential))
	require.NoError(t, err)
	require.Len(t, state.Snapshots, 1)
	for _, snapshot := range state.Snapshots {
		require.Equal(t, "accepted", snapshot.Delivery)
		require.NotNil(t, snapshot.Conflict)
	}
}

func TestZCodeRechecksRouteBeforeRetryingMissingACK(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "usage.sqlite")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER);`)
	require.NoError(t, err)
	now := time.Now()
	_, err = db.Exec(`INSERT INTO model_usage VALUES ('id','session','provider','model','completed',?,?,10,5,2,0,1,15)`, now.UnixMilli(), now.UnixMilli())
	require.NoError(t, err)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, err := w.Write([]byte(`{"success":true,"data":{}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: path, AcceptWindowDays: 90, ZCodeProviderRoutes: []ZCodeProviderRoute{{ProviderID: "provider", Origin: "https://external.example", From: now.Add(-time.Hour).Unix(), Until: now.Add(time.Hour).Unix()}}}
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Equal(t, 1, requests)
	state, err := loadZCodeState(cfg.StatePath, sha256Hex(serverIdentity(cfg.Server)+"\x00"+cfg.Credential))
	require.NoError(t, err)
	for _, snapshot := range state.Snapshots {
		require.Equal(t, "queued", snapshot.Delivery)
	}
	cfg.ZCodeProviderRoutes[0].Origin = server.URL
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Equal(t, 1, requests)
	queueRoot := filepath.Join(cfg.StatePath+".usage-queue", SourceZCode, sha256Hex(serverIdentity(cfg.Server)+"\x00"+cfg.Credential))
	entries, err := os.ReadDir(queueRoot)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "unacknowledged usage must remain durable")
}

func TestZCodeExcludesConfiguredGatewayHostOnNonDefaultPort(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "usage.sqlite")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	now := time.Now()
	_, err = db.Exec(`INSERT INTO model_usage VALUES ('id','session','gateway','model','completed',?,?,10,5,0,0,0,15)`, now.UnixMilli(), now.UnixMilli())
	require.NoError(t, err)
	cfg := Config{Source: SourceZCode, Server: "https://report.example", Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: path, AcceptWindowDays: 90,
		ZCodeGatewayHosts: []string{"token.einyun.com"}, ZCodeProviderRoutes: []ZCodeProviderRoute{{ProviderID: "gateway", Origin: "https://TOKEN.EINYUN.COM:8443/v1", From: now.Add(-time.Hour).Unix(), Until: now.Add(time.Hour).Unix()}}}
	var output strings.Builder
	require.NoError(t, RunClient(context.Background(), cfg, false, &output, io.Discard))
	require.Contains(t, output.String(), "0 sent")
	require.Contains(t, output.String(), "1 excluded")
	state, err := loadZCodeState(cfg.StatePath, sha256Hex(serverIdentity(cfg.Server)+"\x00"+cfg.Credential))
	require.NoError(t, err)
	for _, snapshot := range state.Snapshots {
		require.False(t, snapshot.Submitted)
	}
}
