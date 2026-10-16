package reporterupdate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hostUpdateFixture(t *testing.T) *updateFixture {
	t.Helper()
	f := newUpdateFixture(t)
	f.m.Target = runtime.GOOS + "-" + runtime.GOARCH
	i, _, err := f.m.read()
	require.NoError(t, err)
	data, err := os.ReadFile(i.Current.Binary)
	require.NoError(t, err)
	name, err := TargetName(f.m.Target)
	require.NoError(t, err)
	i.Current.Binary = filepath.Join(f.m.Root, "releases", i.Current.ID, f.m.Target, name)
	require.NoError(t, SecureDir(filepath.Dir(i.Current.Binary)))
	require.NoError(t, os.WriteFile(i.Current.Binary, data, 0o700))
	require.NoError(t, Save(filepath.Join(f.m.Root, "installation.json"), i))
	return f
}

func TestStableLauncherTracksCommittedUpdatesAndRollback(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("managed target platforms only")
	}
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			f := hostUpdateFixture(t)
			i, _, err := f.m.read()
			require.NoError(t, err)
			old := i.Current.Binary
			bytes, err := os.ReadFile(old)
			require.NoError(t, err)
			artifact := Artifact{Target: f.m.Target, SHA256: Digest(bytes), Size: int64(len(bytes))}
			launcher, err := EnsureLauncher(f.m.Root, old, artifact)
			require.NoError(t, err)
			current, err := CurrentBinary(f.m.Root)
			require.NoError(t, err)
			assert.Equal(t, old, current)
			id, err := f.m.CheckAndPrepare(context.Background(), false)
			require.NoError(t, err)
			// The helper remains available while the update lock is held.
			unlock, err := f.m.lock()
			require.NoError(t, err)
			current, err = CurrentBinary(f.m.Root)
			unlock()
			require.NoError(t, err)
			assert.Equal(t, old, current)
			if rollback {
				f.tasks.fail = "task-claude_code"
			}
			err = f.m.Apply(context.Background(), id)
			if rollback {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			current, err = CurrentBinary(f.m.Root)
			require.NoError(t, err)
			if rollback {
				assert.Equal(t, old, current)
			} else {
				assert.Contains(t, current, "release-2")
			}
			// Re-enrollment must retain the bootstrap to which Claude refers.
			candidate, err := EnsureLauncher(f.m.Root, current, Artifact{Target: f.m.Target, SHA256: Digest(f.binary), Size: int64(len(f.binary))})
			require.NoError(t, err)
			assert.Equal(t, launcher, candidate)
			original, err := os.ReadFile(launcher)
			require.NoError(t, err)
			assert.Equal(t, bytes, original)
			require.NoError(t, os.WriteFile(current, []byte("tampered-current"), 0o700))
			_, err = CurrentBinary(f.m.Root)
			require.Error(t, err, "modified current binaries must never execute")
		})
	}
}

func TestLauncherRejectsTamperingAndUncommittedPaths(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("managed target platforms only")
	}
	f := hostUpdateFixture(t)
	i, _, err := f.m.read()
	require.NoError(t, err)
	bytes, err := os.ReadFile(i.Current.Binary)
	require.NoError(t, err)
	a := Artifact{Target: f.m.Target, SHA256: Digest(bytes), Size: int64(len(bytes))}
	path, err := EnsureLauncher(f.m.Root, i.Current.Binary, a)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("tampered-bootstrap"), 0o700))
	_, err = EnsureLauncher(f.m.Root, i.Current.Binary, a)
	require.Error(t, err)
	i.Current.Binary = filepath.Join(f.m.Root, "uncommitted-executable")
	require.NoError(t, Save(filepath.Join(f.m.Root, "installation.json"), i))
	_, err = CurrentBinary(f.m.Root)
	require.Error(t, err)
}
