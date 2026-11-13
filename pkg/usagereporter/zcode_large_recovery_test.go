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

// Exercises the persisted layout of an interrupted large first installation.
func TestZCodeRecoversLargeInterruptedQueue(t *testing.T) {
	if os.Getenv("NEW_API_TEST_ZCODE_LARGE") != "1" {
		t.Skip("opt-in large durable queue integration")
	}
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	var mu sync.Mutex
	accepted := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		if common.DecodeJson(r.Body, &payload) != nil {
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}
		ids := make([]string, 0, len(payload.Events))
		mu.Lock()
		for _, event := range payload.Events {
			accepted[event.EventID]++
			ids = append(ids, event.EventID)
		}
		mu.Unlock()
		body, _ := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		_, _ = w.Write(body)
	}))
	defer server.Close()
	statePath := filepath.Join(root, "state.json")
	providerPath := filepath.Join(root, "providers.json")
	require.NoError(t, os.WriteFile(providerPath, []byte(`{"provider":{"direct":{"options":{"baseURL":"https://external.example"}}}}`), 0o600))
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: statePath, ZCodeDBPath: dbPath, ZCodeProviderConfigPath: providerPath, AcceptWindowDays: 90}
	namespace := sha256Hex(serverIdentity(server.URL) + "\x00" + cfg.Credential)
	state, err := loadZCodeState(statePath, namespace)
	require.NoError(t, err)
	queue := filepath.Join(statePath+".usage-queue", SourceZCode, namespace)
	require.NoError(t, os.MkdirAll(queue, 0o700))
	tx, err := db.Begin()
	require.NoError(t, err)
	stamp := time.Now().UnixMilli()
	for i := 0; i < 9773; i++ {
		id, provider, status := fmt.Sprintf("%05d", i), "direct", "completed"
		if i >= 9592 {
			provider = "unknown"
		}
		if i >= 9705 {
			status = "running"
		}
		_, err = tx.Exec(`INSERT INTO model_usage VALUES (?, 'session', ?, 'model', ?, ?, ?, 10, 5, 0, 0, 0, 15)`, id, provider, status, stamp, stamp)
		require.NoError(t, err)
		if i >= 9705 {
			continue
		}
		row := zcodeUsageRow{ID: id, SessionID: "session", ProviderID: provider, ModelID: "model", Status: status, StartedAt: sql.NullInt64{Int64: stamp, Valid: true}, CompletedAt: sql.NullInt64{Int64: stamp, Valid: true}, Input: 10, Output: 5, Total: 15}
		event, _, e := normalizeZCodeUsage(row)
		require.NoError(t, e)
		state.Snapshots[event.EventID] = zcodeSnapshot{Event: event, ProviderID: provider, StartedAt: stamp / 1000, Held: i < 9592}
		if i < 8438 {
			record, e := common.Marshal(CollectedEvent{Event: event, RouteID: sha256Hex(provider), Origin: "https://external.example"})
			require.NoError(t, e)
			require.NoError(t, os.WriteFile(filepath.Join(queue, event.EventID+".json"), record, 0o600))
		}
	}
	require.NoError(t, tx.Commit())
	require.NoError(t, state.save(statePath))
	healthy := make(chan time.Duration, 1)
	var once sync.Once
	start := time.Now()
	ctx := WithHealthObserver(context.Background(), func() { once.Do(func() { healthy <- time.Since(start) }) })
	var output strings.Builder
	require.NoError(t, RunClient(ctx, cfg, false, &output, io.Discard))
	select {
	case elapsed := <-healthy:
		t.Logf("health after first durable progress: %s; complete: %s", elapsed, time.Since(start))
		require.Less(t, elapsed, 90*time.Second)
	default:
		t.Fatal("healthy progress was not signalled")
	}
	mu.Lock()
	require.Len(t, accepted, 9592)
	for _, count := range accepted {
		require.Equal(t, 1, count)
	}
	mu.Unlock()
	require.Contains(t, output.String(), "113 unresolved")
	restored, err := loadZCodeState(statePath, namespace)
	require.NoError(t, err)
	require.Len(t, restored.Snapshots, 113)
	for _, snapshot := range restored.Snapshots {
		require.Equal(t, "unknown", snapshot.ProviderID)
		require.False(t, snapshot.Submitted)
	}
	archive, err := openZCodeArchive(statePath, namespace, false, true)
	require.NoError(t, err)
	require.NotNil(t, archive)
	var archived int
	require.NoError(t, archive.db.QueryRow(`SELECT count(*) FROM snapshots`).Scan(&archived))
	require.Equal(t, 9592, archived)
	require.NoError(t, archive.close())
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	mu.Lock()
	require.Len(t, accepted, 9592)
	for _, count := range accepted {
		require.Equal(t, 1, count, "accepted usage must not be sent again")
	}
	mu.Unlock()
	tx, err = db.Begin()
	require.NoError(t, err)
	for i := 9773; i < 10173; i++ {
		_, err = tx.Exec(`INSERT INTO model_usage VALUES (?, 'session', 'direct', 'model', 'completed', ?, ?, 10, 5, 0, 0, 0, 15)`, fmt.Sprintf("%05d", i), stamp, stamp)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	_, err = db.Exec(`UPDATE model_usage SET input_tokens=11, computed_total_tokens=16 WHERE id='00000'`)
	require.NoError(t, err)
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	mu.Lock()
	require.Len(t, accepted, 9992, "new events after archival must continue to be accepted")
	for _, count := range accepted {
		require.Equal(t, 1, count)
	}
	mu.Unlock()
	restored, err = loadZCodeState(statePath, namespace)
	require.NoError(t, err)
	conflictID := sha256Hex("zcode:model_usage:v1\x00session\x0000000")
	require.NotNil(t, restored.Snapshots[conflictID].Conflict, "revisions to archived events must be quarantined")
}

func TestZCodeFreshLargeHistory(t *testing.T) {
	if os.Getenv("NEW_API_TEST_ZCODE_LARGE") != "1" {
		t.Skip("opt-in large first installation integration")
	}
	root := t.TempDir()
	dbPath := filepath.Join(root, "source.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	stamp := time.Now().UnixMilli()
	for i := 0; i < 9773; i++ {
		provider, status := "direct", "completed"
		if i >= 9592 {
			provider = "unknown"
		}
		if i >= 9705 {
			status = "running"
		}
		_, err = tx.Exec(`INSERT INTO model_usage VALUES (?, 'session', ?, 'model', ?, ?, ?, 10, 5, 0, 0, 0, 15)`, fmt.Sprintf("%05d", i), provider, status, stamp, stamp)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	var mu sync.Mutex
	accepted := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		if common.DecodeJson(r.Body, &payload) != nil {
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}
		var ids []string
		mu.Lock()
		for _, event := range payload.Events {
			accepted[event.EventID]++
			ids = append(ids, event.EventID)
		}
		mu.Unlock()
		body, _ := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		_, _ = w.Write(body)
	}))
	defer server.Close()
	providerPath := filepath.Join(root, "providers.json")
	require.NoError(t, os.WriteFile(providerPath, []byte(`{"provider":{"direct":{"options":{"baseURL":"https://external.example"}}}}`), 0o600))
	cfg := Config{Source: SourceZCode, Server: server.URL, Credential: "fixture", StatePath: filepath.Join(root, "state.json"), ZCodeDBPath: dbPath, ZCodeProviderConfigPath: providerPath, AcceptWindowDays: 90}
	healthy := make(chan time.Duration, 1)
	var once sync.Once
	start := time.Now()
	ctx := WithHealthObserver(context.Background(), func() { once.Do(func() { healthy <- time.Since(start) }) })
	var output strings.Builder
	require.NoError(t, RunClient(ctx, cfg, false, &output, io.Discard))
	select {
	case elapsed := <-healthy:
		t.Logf("health after first durable progress: %s; complete: %s", elapsed, time.Since(start))
		require.Less(t, elapsed, 90*time.Second)
	default:
		t.Fatal("healthy progress was not signalled")
	}
	mu.Lock()
	require.Len(t, accepted, 9592)
	for _, count := range accepted {
		require.Equal(t, 1, count)
	}
	mu.Unlock()
	require.Contains(t, output.String(), "113 unresolved")
	require.NoError(t, RunClient(context.Background(), cfg, false, io.Discard, io.Discard))
	mu.Lock()
	defer mu.Unlock()
	for _, count := range accepted {
		require.Equal(t, 1, count, "accepted usage must not be sent again")
	}
}
