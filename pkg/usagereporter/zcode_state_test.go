package usagereporter

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZCodeStateKeepsUnknownAndConflictingSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state, err := loadZCodeState(path, "namespace")
	require.NoError(t, err)
	event := UsageEvent{EventID: "id", InputTokens: 10, TotalTokens: 10}
	require.NoError(t, state.observe(event, "provider", 1))
	require.NoError(t, state.save(path))
	state, err = loadZCodeState(path, "namespace")
	require.NoError(t, err)
	require.Equal(t, event, state.Snapshots["id"].Event)
	snapshot := state.Snapshots["id"]
	snapshot.Held = true
	state.Snapshots["id"] = snapshot
	event.InputTokens = 11
	event.TotalTokens = 11
	require.NoError(t, state.observe(event, "other-provider", 1))
	require.NoError(t, state.save(path))
	state, err = loadZCodeState(path, "namespace")
	require.NoError(t, err)
	require.Equal(t, 10, state.Snapshots["id"].Event.InputTokens)
	require.Equal(t, 11, state.Snapshots["id"].Conflict.Event.InputTokens)
	require.Equal(t, "other-provider", state.Snapshots["id"].Conflict.ProviderID)
	_, err = loadZCodeState(path, "new-credential-namespace")
	require.Error(t, err)
}

func TestZCodeArchivesOnlyConfirmedSnapshotsAndDetectsRevisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state, err := loadZCodeState(path, "namespace")
	require.NoError(t, err)
	for i := 0; i < zcodeArchiveThreshold; i++ {
		id := fmt.Sprintf("id-%05d", i)
		state.Snapshots[id] = zcodeSnapshot{Event: UsageEvent{EventID: id, InputTokens: 10, TotalTokens: 10}, ProviderID: "provider", StartedAt: 1, Held: true, Submitted: true, Delivery: "accepted"}
	}
	state.Snapshots["unknown"] = zcodeSnapshot{Event: UsageEvent{EventID: "unknown"}, ProviderID: "unknown"}
	state.Snapshots["rejected"] = zcodeSnapshot{Event: UsageEvent{EventID: "rejected"}, Held: true, Submitted: true, Delivery: "rejected"}
	state.Snapshots["conflict"] = zcodeSnapshot{Event: UsageEvent{EventID: "conflict"}, Held: true, Submitted: true, Delivery: "accepted", Conflict: &zcodeConflict{}}
	require.NoError(t, state.save(path))
	beforeCompaction, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, state.compact(path))
	require.Len(t, state.Snapshots, 3, "unknown, rejected and conflicted records remain active")
	info, err := os.Stat(path + ".archive.sqlite")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.NoError(t, state.archive.close())
	// Simulate a crash after the archive commit but before the JSON checkpoint.
	require.NoError(t, os.WriteFile(path, beforeCompaction, 0o600))
	crashed, err := loadZCodeState(path, "namespace")
	require.NoError(t, err)
	crashed.archive, err = openZCodeArchive(path, "namespace", false, false)
	require.NoError(t, err)
	require.NoError(t, crashed.compact(path))
	require.NoError(t, crashed.archive.close())
	restored, err := loadZCodeState(path, "namespace")
	require.NoError(t, err)
	require.Equal(t, 2, restored.Version)
	require.True(t, restored.ArchiveRequired)
	require.EqualValues(t, zcodeArchiveThreshold, restored.ArchiveCount)
	restored.archive, err = openZCodeArchive(path, "namespace", false, false)
	require.NoError(t, err)
	defer restored.archive.close()
	var count int
	require.NoError(t, restored.archive.db.QueryRow(`SELECT count(*) FROM snapshots`).Scan(&count))
	require.Equal(t, zcodeArchiveThreshold, count)
	event := UsageEvent{EventID: "id-00000", InputTokens: 10, TotalTokens: 10}
	require.NoError(t, restored.observe(event, "provider", 1))
	require.NotContains(t, restored.Snapshots, event.EventID, "an identical archived record must not be requeued")
	event.InputTokens, event.TotalTokens = 11, 11
	require.NoError(t, restored.observe(event, "provider", 1))
	require.NotNil(t, restored.Snapshots[event.EventID].Conflict, "a changed archived record must remain quarantined")
	require.Equal(t, 10, restored.Snapshots[event.EventID].Event.InputTokens)
	require.Equal(t, 11, restored.Snapshots[event.EventID].Conflict.Event.InputTokens)
	require.NoError(t, restored.save(path))
	rechecked, err := loadZCodeState(path, "namespace")
	require.NoError(t, err)
	require.NotNil(t, rechecked.Snapshots[event.EventID].Conflict)
	_, err = openZCodeArchive(path, "different-namespace", false, true)
	require.ErrorContains(t, err, "identity")
	_, err = restored.archive.db.Exec(`DELETE FROM snapshots WHERE event_id = ?`, "id-00001")
	require.NoError(t, err)
	require.ErrorContains(t, restored.validateArchive(restored.archive), "incomplete")
}

func TestZCodeMissingRequiredArchiveStopsCollection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	server := "https://gateway.example"
	credential := "fixture"
	state, err := loadZCodeState(path, sha256Hex(serverIdentity(server)+"\x00"+credential))
	require.NoError(t, err)
	state.Version, state.ArchiveRequired, state.ArchiveCount = 2, true, 1
	require.NoError(t, state.save(path))
	cfg := Config{Server: server, Source: SourceZCode, Credential: credential, StatePath: path}
	err = runZCodeCollector(context.Background(), cfg, io.Discard, io.Discard)
	require.ErrorContains(t, err, "required ZCode archive is missing")
	_, err = os.Stat(path + ".archive.sqlite")
	require.True(t, os.IsNotExist(err), "missing history must not be silently replaced")
}

func TestZCodeInvalidArchiveStopsBeforeCollection(t *testing.T) {
	for _, kind := range []string{"corrupt", "wrong_identity"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "state.json")
			server, credential := "https://gateway.example", "fixture"
			state, err := loadZCodeState(path, sha256Hex(serverIdentity(server)+"\x00"+credential))
			require.NoError(t, err)
			state.Version, state.ArchiveRequired, state.ArchiveCount = 2, true, 1
			require.NoError(t, state.save(path))
			if kind == "corrupt" {
				require.NoError(t, os.WriteFile(path+".archive.sqlite", []byte("corrupt fixture"), 0o600))
			} else {
				archive, e := openZCodeArchive(path, "other-namespace", true, false)
				require.NoError(t, e)
				require.NoError(t, archive.close())
			}
			before, err := os.ReadFile(path + ".archive.sqlite")
			require.NoError(t, err)
			err = runZCodeCollector(context.Background(), Config{Server: server, Source: SourceZCode, Credential: credential, StatePath: path}, io.Discard, io.Discard)
			require.ErrorContains(t, err, "invalid ZCode archive")
			after, err := os.ReadFile(path + ".archive.sqlite")
			require.NoError(t, err)
			require.Equal(t, before, after, "invalid archive must not be replaced")
		})
	}
}
