package usagereporter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reporterTestEvent(id, origin string) CollectedEvent {
	return CollectedEvent{Origin: origin, ClientVersion: "client-fixture", Event: UsageEvent{
		EventID: sha256Hex(id), SessionID: sha256Hex("session"), OccurredAt: time.Now().Add(-time.Minute).Unix(),
		SourceModelName: "same-model", InputTokens: 10, CachedInputTokens: 2, OutputTokens: 5, TotalTokens: 15,
	}}
}

func TestReporterFiltersAndUploadsWithoutClientSpecificImplementation(t *testing.T) {
	for _, source := range []string{SourceCodex, SourceClaudeCode, "future_client"} {
		t.Run(source, func(t *testing.T) {
			var uploads []ReportPayload
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/external-usage/report", r.URL.Path)
				assert.Equal(t, "Bearer reporting-fixture", r.Header.Get("Authorization"))
				var payload ReportPayload
				require.NoError(t, common.DecodeJson(r.Body, &payload))
				uploads = append(uploads, payload)
				require.Len(t, payload.Events, 1)
				body, err := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: []string{payload.Events[0].EventID}}})
				require.NoError(t, err)
				_, err = w.Write(body)
				require.NoError(t, err)
			}))
			defer server.Close()
			cfg := ReporterConfig{Source: source, Server: server.URL, Credential: "reporting-fixture", StatePath: filepath.Join(t.TempDir(), "state.json"),
				GatewayURLs: []string{"https://token.example"}, ExternalURLs: []string{"https://api.external.example", "https://token.example"}}
			reporter, err := NewReporter(cfg)
			require.NoError(t, err)
			direct := reporterTestEvent("direct", "https://api.external.example/v1")
			committed := false
			stats, err := reporter.Report(context.Background(), Collection{Events: []CollectedEvent{
				reporterTestEvent("gateway", "https://TOKEN.example:443/v1/responses"),
				reporterTestEvent("server", server.URL+"/v1"), direct,
			}, Commit: func() error { committed = true; return nil }})
			require.NoError(t, err)
			assert.True(t, committed)
			assert.Equal(t, ReportStats{Sent: 1, Accepted: 1}, stats)
			require.Len(t, uploads, 1)
			assert.Equal(t, source, uploads[0].Source)
			assert.Equal(t, []UsageEvent{direct.Event}, uploads[0].Events)
			wire, err := common.Marshal(uploads[0])
			require.NoError(t, err)
			assert.NotContains(t, string(wire), "upstream_origin")
			assert.NotContains(t, string(wire), cfg.Credential)
			assert.NotContains(t, string(wire), "api.external.example")
			err = reporter.Record(reporterTestEvent("unknown", "https://api.external.example.attacker.test"))
			assert.ErrorIs(t, err, ErrUnapprovedRoute)
			require.NoError(t, reporter.Flush())
			assert.Len(t, uploads, 1, "missing/unapproved routes and empty queues cannot upload")
		})
	}
}

func TestReporterKeepsIncompleteACKsAcrossBatchesAndRestart(t *testing.T) {
	var uploads []ReportPayload
	var records []CollectedEvent
	var permanentlyRejected CollectedEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		uploads = append(uploads, payload)
		ack := ReportAcknowledgement{}
		switch len(uploads) {
		case 1:
			require.Len(t, payload.Events, 100)
			for _, event := range payload.Events[1:] {
				ack.AcceptedEventIDs = append(ack.AcceptedEventIDs, event.EventID)
			}
			// A valid ID from a later batch is still outside this ACK's scope.
			for _, record := range records {
				found := false
				for _, event := range payload.Events {
					if record.Event.EventID == event.EventID {
						found = true
					}
				}
				if !found {
					ack.AcceptedEventIDs = append(ack.AcceptedEventIDs, record.Event.EventID)
				}
			}
		case 2:
			require.Len(t, payload.Events, 2)
			ack.DuplicateEventIDs = []string{payload.Events[0].EventID}
		case 3:
			require.Len(t, payload.Events, 2, "both unacknowledged events must survive restart")
			ack.AcceptedEventIDs = []string{payload.Events[0].EventID}
			ack.Rejected = []RejectedEvent{{EventID: payload.Events[1].EventID, Reason: "private-remote-reason"}}
			for _, record := range records {
				if record.Event.EventID == payload.Events[1].EventID {
					permanentlyRejected = record
				}
			}
		default:
			t.Error("unexpected upload after explicit ACK")
		}
		body, err := common.Marshal(map[string]any{"success": true, "data": ack})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	cfg := ReporterConfig{Source: "future_client", Server: server.URL, Credential: "fixture", StatePath: filepath.Join(t.TempDir(), "state.json")}
	reporter, err := NewReporter(cfg)
	require.NoError(t, err)
	for i := 0; i < 102; i++ {
		record := reporterTestEvent(fmt.Sprint(i), "https://api.external.example")
		records = append(records, record)
		require.NoError(t, reporter.Record(record))
	}
	stats, err := reporter.FlushContext(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ReportStats{Sent: 102, Accepted: 99, Duplicate: 1}, stats)
	restarted, err := NewReporter(cfg)
	require.NoError(t, err)
	require.NoError(t, restarted.Flush())
	require.Len(t, uploads, 3)
	require.NotEmpty(t, permanentlyRejected.Event.EventID)
	require.NoError(t, restarted.Record(permanentlyRejected))
	require.NoError(t, restarted.Flush())
	assert.Len(t, uploads, 3, "permanent rejections must not be re-enqueued")
	conflict := permanentlyRejected
	conflict.Event.TotalTokens++
	assert.ErrorIs(t, restarted.Record(conflict), ErrUsageConflict)
	require.NoError(t, filepath.WalkDir(cfg.StatePath+".usage-queue", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		assert.NotContains(t, string(content), "private-remote-reason")
		return nil
	}))
}

func TestReporterRetriesDurablyBeforeCommittingCollection(t *testing.T) {
	var uploads []ReportPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		uploads = append(uploads, payload)
		if len(uploads) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		body, err := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: []string{payload.Events[0].EventID}}})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	cfg := ReporterConfig{Source: "future_client", Server: server.URL, Credential: "fixture", StatePath: filepath.Join(t.TempDir(), "state.json")}
	reporter, err := NewReporter(cfg)
	require.NoError(t, err)
	committed := false
	commit := func() error { committed = true; return nil }
	_, err = reporter.Report(context.Background(), Collection{Events: []CollectedEvent{reporterTestEvent("retry", "https://api.external.example")}, Commit: commit})
	require.Error(t, err)
	assert.False(t, committed)
	restarted, err := NewReporter(cfg)
	require.NoError(t, err)
	_, err = restarted.Report(context.Background(), Collection{Commit: commit})
	require.NoError(t, err)
	assert.True(t, committed)
	require.Len(t, uploads, 2)
	assert.Equal(t, uploads[0].Events, uploads[1].Events, "an adapter need not recollect pending data after a restart")
}

func TestReporterIsolationAndInvalidMetadata(t *testing.T) {
	cfg := ReporterConfig{Source: SourceCodex, Server: "https://token.example", Credential: "fixture", StatePath: filepath.Join(t.TempDir(), "state.json")}
	reporter, err := NewReporter(cfg)
	require.NoError(t, err)
	for _, change := range []func(*CollectedEvent){
		func(r *CollectedEvent) { r.Origin = "" },
		func(r *CollectedEvent) { r.Event.EventID = "../unsafe" },
		func(r *CollectedEvent) { r.Event.InputTokens = -1 },
		func(r *CollectedEvent) { r.Event.TotalTokens = 0 },
		func(r *CollectedEvent) { oversized := int64(2147483648); r.Event.TotalTokens = int(oversized) },
		func(r *CollectedEvent) { r.Event.OccurredAt = time.Now().Add(time.Hour).Unix() },
		func(r *CollectedEvent) { r.Event.SourceModelName = "" },
	} {
		record := reporterTestEvent("invalid", "https://api.external.example")
		change(&record)
		require.Error(t, reporter.Record(record))
	}
	for _, change := range []func(*ReporterConfig){
		func(c *ReporterConfig) { c.Source = SourceClaudeCode },
		func(c *ReporterConfig) { c.Server = "https://other.example" },
		func(c *ReporterConfig) { c.Credential = "other-account" },
	} {
		otherConfig := cfg
		change(&otherConfig)
		other, err := NewReporter(otherConfig)
		require.NoError(t, err)
		assert.NotEqual(t, reporter.queueDir, other.queueDir, "source, server and account each isolate pending data")
	}
	cfg.ExternalURLs = []string{}
	denied, err := NewReporter(cfg)
	require.NoError(t, err)
	assert.ErrorIs(t, denied.Record(reporterTestEvent("deny", "https://api.external.example")), ErrUnapprovedRoute)
}

func TestReporterCancellationRetainsPendingData(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()
	cfg := ReporterConfig{Source: "future_client", Server: server.URL, Credential: "fixture", StatePath: filepath.Join(t.TempDir(), "state.json")}
	reporter, err := NewReporter(cfg)
	require.NoError(t, err)
	event := reporterTestEvent("cancelled", "https://api.external.example")
	require.NoError(t, reporter.Record(event))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := reporter.FlushContext(ctx); finished <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("upload did not start")
	}
	cancel()
	select {
	case err := <-finished:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Error("upload did not stop after cancellation")
	}
	close(release)
	restarted, err := NewReporter(cfg)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(restarted.queueDir, event.Event.EventID+".json"))
	require.NoError(t, err, "cancelled uploads must leave the same event durable")
	var queued CollectedEvent
	require.NoError(t, common.Unmarshal(content, &queued))
	assert.Equal(t, event, queued)
}

func TestSharedReporterReadsLegacyClaudeQueueWithoutChangingIdentity(t *testing.T) {
	var payload ReportPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		body, err := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: []string{payload.Events[0].EventID}}})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	cfg := claudeReceiverConfig(t, server.URL)
	legacyDir := filepath.Join(cfg.StatePath+".claude-queue", sha256Hex(server.URL+"\x00"+cfg.Credential))
	require.NoError(t, os.MkdirAll(legacyDir, 0o700))
	event := reporterTestEvent("legacy", "https://api.anthropic.com")
	wireEvent, err := common.Marshal(event.Event)
	require.NoError(t, err)
	legacyJSON := fmt.Sprintf(`{"event":%s,"client_version":"2.0","upstream_origin":"https://api.anthropic.com"}`, wireEvent)
	path := filepath.Join(legacyDir, event.Event.EventID+".json")
	require.NoError(t, os.WriteFile(path, []byte(legacyJSON), 0o600))
	receiver, err := NewClaudeReceiver(cfg)
	require.NoError(t, err)
	require.NoError(t, receiver.Flush())
	assert.Equal(t, SourceClaudeCode, payload.Source)
	assert.Equal(t, []UsageEvent{event.Event}, payload.Events)
	assert.Equal(t, "2.0", payload.ClientVersion)
	assert.NoFileExists(t, path)
}

func TestReporterDrainsFullQueueAndResumesPartiallyAcknowledgedScan(t *testing.T) {
	var uploads []ReportPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ReportPayload
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		uploads = append(uploads, payload)
		if len(uploads) == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		ids := make([]string, 0, len(payload.Events))
		for _, event := range payload.Events {
			ids = append(ids, event.EventID)
		}
		body, err := common.Marshal(map[string]any{"success": true, "data": ReportAcknowledgement{AcceptedEventIDs: ids}})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	cfg := ReporterConfig{Source: "future_client", Server: server.URL, Credential: "fixture", StatePath: filepath.Join(t.TempDir(), "state.json"), QueueCapacity: 2}
	reporter, err := NewReporter(cfg)
	require.NoError(t, err)
	events := []CollectedEvent{
		reporterTestEvent("one", "https://api.external.example"), reporterTestEvent("two", "https://api.external.example"),
		reporterTestEvent("three", "https://api.external.example"), reporterTestEvent("four", "https://api.external.example"),
		reporterTestEvent("five", "https://api.external.example"),
	}
	committed := false
	collection := Collection{Events: events, Commit: func() error { committed = true; return nil }}
	stats, err := reporter.Report(context.Background(), collection)
	require.Error(t, err)
	assert.False(t, committed)
	assert.Equal(t, ReportStats{Sent: 2, Accepted: 2}, stats)
	restarted, err := NewReporter(cfg)
	require.NoError(t, err)
	stats, err = restarted.Report(context.Background(), collection)
	require.NoError(t, err)
	assert.True(t, committed)
	assert.Equal(t, ReportStats{Sent: 3, Accepted: 3}, stats, "successful ACK receipts prevent rescanning an accepted prefix after restart")
	require.Len(t, uploads, 4)
	assert.Equal(t, uploads[1].Events, uploads[2].Events, "only the failed batch is retried")
	assert.NoFileExists(t, filepath.Join(restarted.queueDir, ".confirmed"), "the source checkpoint retires temporary ACK receipts")
}
