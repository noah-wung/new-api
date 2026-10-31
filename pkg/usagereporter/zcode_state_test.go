package usagereporter

import (
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
