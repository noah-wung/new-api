package usagereporter

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestZCodeAccountRuleUnblocksRetainedUsageWithoutGatewayDuplicates(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	now := time.Now().UnixMilli()
	for _, item := range []struct{ id, provider string }{{"direct", "account:direct"}, {"gateway", "account:gateway"}, {"unknown", "account:unknown"}} {
		_, err = db.Exec(`INSERT INTO model_usage VALUES (?, 'session', ?, 'model', 'completed', ?, ?, 10, 5, 0, 0, 0, 15)`, item.id, item.provider, now, now)
		require.NoError(t, err)
	}
	var received []ReportPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		received = append(received, payload)
		var ids []string
		for _, event := range payload.Events {
			ids = append(ids, event.EventID)
		}
		wire, e := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		require.NoError(t, e)
		_, e = w.Write(wire)
		require.NoError(t, e)
	}))
	defer server.Close()
	builtin := filepath.Join(root, "builtin.json")
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath, ZCodeProviderConfigPath: filepath.Join(root, "config.json"), ZCodeBuiltinProviderConfigPath: builtin, ZCodeGatewayHosts: []string{"token.example"}, AcceptWindowDays: 90}
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Empty(t, received, "without a known origin all rows must remain unresolved")
	require.NoError(t, os.WriteFile(builtin, []byte(`{"config":{"providerConfigRules":{"providerRules":[{"providerId":"account:direct","config":{"api":{"baseUrl":"https://official.example/v1"}}},{"providerId":"account:gateway","config":{"api":{"baseUrl":"https://token.example/v1"}}}]}}}`), 0600))
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Len(t, received, 1)
	require.Len(t, received[0].Events, 1, "only the known direct usage is sent")
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Len(t, received, 1, "an acknowledged event must not be sent again")
	state, err := loadZCodeState(cfg.StatePath, sha256Hex(serverIdentity(cfg.Server)+"\x00"+cfg.Credential))
	require.NoError(t, err)
	for _, snapshot := range state.Snapshots {
		if snapshot.ProviderID == "account:direct" {
			require.True(t, snapshot.Submitted)
			require.Equal(t, "accepted", snapshot.Delivery)
		} else {
			require.False(t, snapshot.Submitted)
		}
	}
}

func TestZCodeSignalsHealthyAfterFirstDurableBatch(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	now := time.Now().UnixMilli()
	for i := 0; i < 101; i++ {
		_, err = db.Exec(`INSERT INTO model_usage VALUES (?, 'session', 'provider', 'model', 'completed', ?, ?, 10, 5, 0, 0, 0, 15)`, fmt.Sprintf("%03d", i), now, now)
		require.NoError(t, err)
	}
	providerPath := filepath.Join(root, "providers.json")
	require.NoError(t, os.WriteFile(providerPath, []byte(`{"provider":{"provider":{"options":{"baseURL":"https://external.example"}}}}`), 0o600))
	secondStarted := make(chan struct{})
	release := make(chan struct{})
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		if common.DecodeJson(r.Body, &payload) != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		requests++
		if requests == 2 {
			close(secondStarted)
			<-release
		}
		ids := make([]string, 0, len(payload.Events))
		for _, event := range payload.Events {
			ids = append(ids, event.EventID)
		}
		body, _ := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		_, _ = w.Write(body)
	}))
	defer server.Close()
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath, ZCodeProviderConfigPath: providerPath, AcceptWindowDays: 90}
	healthy := make(chan struct{})
	var once sync.Once
	ctx := WithHealthObserver(context.Background(), func() { once.Do(func() { close(healthy) }) })
	done := make(chan error, 1)
	go func() { done <- RunClient(ctx, cfg, false, io.Discard, io.Discard) }()
	select {
	case <-secondStarted:
	case <-time.After(10 * time.Second):
		close(release)
		<-done
		t.Fatal("second report batch did not start")
	}
	readyBeforeSecond := false
	select {
	case <-healthy:
		readyBeforeSecond = true
	case <-time.After(2 * time.Second):
	}
	close(release)
	require.NoError(t, <-done)
	require.True(t, readyBeforeSecond, "the first durably acknowledged batch should establish health without waiting for all history")
	state, err := loadZCodeState(cfg.StatePath, sha256Hex(serverIdentity(cfg.Server)+"\x00"+cfg.Credential))
	require.NoError(t, err)
	require.Len(t, state.Snapshots, 101)
	for _, snapshot := range state.Snapshots {
		require.True(t, snapshot.Submitted)
		require.Equal(t, "accepted", snapshot.Delivery)
	}
}

func TestZCodeResumesAfterLaterBatchDeliveryFails(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	stamp := time.Now().UnixMilli()
	for i := 0; i < 101; i++ {
		_, err = db.Exec(`INSERT INTO model_usage VALUES (?, 'session', 'provider', 'model', 'completed', ?, ?, 10, 5, 0, 0, 0, 15)`, fmt.Sprintf("%03d", i), stamp, stamp)
		require.NoError(t, err)
	}
	providerPath := filepath.Join(root, "providers.json")
	require.NoError(t, os.WriteFile(providerPath, []byte(`{"provider":{"provider":{"options":{"baseURL":"https://external.example"}}}}`), 0o600))
	requests := 0
	accepted := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		if common.DecodeJson(r.Body, &payload) != nil {
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}
		requests++
		if requests == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var ids []string
		for _, event := range payload.Events {
			accepted[event.EventID]++
			ids = append(ids, event.EventID)
		}
		body, _ := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		_, _ = w.Write(body)
	}))
	defer server.Close()
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath, ZCodeProviderConfigPath: providerPath, AcceptWindowDays: 90}
	err = RunClient(context.Background(), cfg, false, io.Discard, io.Discard)
	var delivery *DeliveryError
	require.ErrorAs(t, err, &delivery)
	require.Len(t, accepted, 100)
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Len(t, accepted, 101)
	for _, count := range accepted {
		require.Equal(t, 1, count, "already acknowledged events must not be sent again")
	}
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Equal(t, 3, requests)
}

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

func TestZCodeCurrentProvidersClassifyHistoricalUsageAndReplayUnknown(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	when := time.Now().Add(-24 * time.Hour).UnixMilli()
	for _, id := range []string{"gateway", "direct", "missing", "excluded"} {
		_, err = db.Exec(`INSERT INTO model_usage VALUES (?, 'session', ?, 'same-model', 'completed', ?, ?, 10, 5, 0, 0, 0, 15)`, id, id, when, when)
		require.NoError(t, err)
	}
	configPath := filepath.Join(root, "providers.json")
	writeProviders := func(content string) { require.NoError(t, os.WriteFile(configPath, []byte(content), 0600)) }
	writeProviders(`{"provider":{"gateway":{"options":{"baseURL":"http://TOKEN.EINYUN.COM:8080/v1", "apiKey":"never-send-secret"}},"direct":{"options":{"baseURL":"https://direct.example/v1"}},"excluded":{"options":{"baseURL":"https://direct.example/v1"}}}}`)
	var payloads []ReportPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		payloads = append(payloads, payload)
		var ids []string
		for _, event := range payload.Events {
			ids = append(ids, event.EventID)
		}
		wire, e := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		require.NoError(t, e)
		_, e = w.Write(wire)
		require.NoError(t, e)
	}))
	defer server.Close()
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath, ZCodeProviderConfigPath: configPath, ZCodeGatewayHosts: []string{"token.einyun.com"}, ZCodeExcludedProviders: []string{"excluded"}, AcceptWindowDays: 90}
	var output strings.Builder
	require.NoError(t, RunClient(context.Background(), cfg, false, &output, io.Discard))
	require.Len(t, payloads, 1)
	require.Len(t, payloads[0].Events, 2)
	require.Contains(t, output.String(), "2 accepted")
	require.Contains(t, output.String(), "1 unresolved, 1 excluded")
	wire, err := common.Marshal(payloads[0])
	require.NoError(t, err)
	for _, secret := range []string{"never-send-secret", "direct.example", "token.einyun.com", "gateway", "direct"} {
		require.NotContains(t, string(wire), secret)
	}
	writeProviders(`{"provider":{"gateway":{"options":{"baseURL":"https://token.einyun.com/v1"}},"direct":{"options":{"baseURL":"https://direct.example/v1"}},"excluded":{"options":{"baseURL":"https://direct.example/v1"}},"missing":{"options":{"baseURL":"https://another.example/v1"}}}}`)
	output.Reset()
	require.NoError(t, RunClient(context.Background(), cfg, false, &output, io.Discard))
	require.Len(t, payloads, 2)
	require.Len(t, payloads[1].Events, 1)
	require.Contains(t, output.String(), "1 accepted")
	output.Reset()
	require.NoError(t, RunClient(context.Background(), cfg, false, &output, io.Discard))
	require.Len(t, payloads, 2, "previously accepted usage must not be sent twice")
}

func TestZCodeCurrentProviderChangeRechecksQueuedUsage(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	when := time.Now().UnixMilli()
	_, err = db.Exec(`INSERT INTO model_usage VALUES ('id','session','provider','model','completed',?,?,10,5,0,0,0,15)`, when, when)
	require.NoError(t, err)
	providerPath := filepath.Join(root, "providers.json")
	writeProvider := func(base string) {
		data, e := common.Marshal(map[string]any{"provider": map[string]any{"provider": map[string]any{"options": map[string]string{"baseURL": base}}}})
		require.NoError(t, e)
		require.NoError(t, os.WriteFile(providerPath, data, 0600))
	}
	writeProvider("https://direct.example")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, e := w.Write([]byte(`{"success":true,"data":{}}`))
		require.NoError(t, e)
	}))
	defer server.Close()
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath, ZCodeProviderConfigPath: providerPath, ZCodeGatewayHosts: []string{"token.einyun.com"}, AcceptWindowDays: 90}
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Equal(t, 1, requests)
	writeProvider("https://token.einyun.com/v1")
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	require.Equal(t, 1, requests, "unacknowledged usage must not be retried after the route changes to the gateway")
	queueRoot := filepath.Join(cfg.StatePath+".usage-queue", SourceZCode, sha256Hex(serverIdentity(cfg.Server)+"\x00"+cfg.Credential))
	entries, err := os.ReadDir(queueRoot)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
}
