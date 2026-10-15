package reporterupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycleControlReportsIdentityAndCancelsWorker(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, closeControl, e := ServeControl(ctx, config, Health{PID: os.Getpid(), Source: "future_client", ReleaseID: "release-2", Transaction: "transaction-2"}, cancel)
	require.NoError(t, e)
	defer closeControl()
	h, e := QueryControl(context.Background(), config, "health")
	require.NoError(t, e)
	assert.False(t, h.Ready)
	assert.Equal(t, "future_client", h.Source)
	ready()
	h, e = QueryControl(context.Background(), config, "health")
	require.NoError(t, e)
	assert.True(t, h.Ready)
	assert.Equal(t, "transaction-2", h.Transaction)
	_, e = QueryControl(context.Background(), config, "stop")
	require.NoError(t, e)
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not cancel the worker")
	}
}
func TestLiveUpdateLockCannotBeStolenByFileAge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.lock")
	release, e := Lock(path)
	require.NoError(t, e)
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
	_, e = Lock(path)
	assert.Error(t, e, "a running owner retains its OS lock regardless of file age")
	release()
	release, e = Lock(path)
	require.NoError(t, e)
	release()
}
func TestCatalogRotationRequiresCurrentTrustAndCompatibleFormats(t *testing.T) {
	f := newUpdateFixture(t)
	for _, kind := range []string{"expired", "future", "format", "path", "missing-target"} {
		t.Run(kind, func(t *testing.T) {
			c := f.catalog
			c.Targets = append([]Artifact(nil), f.catalog.Targets...)
			switch kind {
			case "expired":
				c.ExpiresAt = time.Now().Add(-time.Second).Unix()
			case "future":
				c.IssuedAt = time.Now().Add(time.Hour).Unix()
			case "format":
				c.Queue = Range{2, 2}
			case "path":
				c.Targets[0].Path = "https://other.example/binary"
			case "missing-target":
				c.Targets = c.Targets[:2]
			}
			data, e := Sign(c, "key-1", f.key)
			require.NoError(t, e)
			_, e = Verify(data, f.keys, time.Now())
			assert.Error(t, e)
		})
	}
	_, e := Verify(f.envelope, TrustKeys{}, time.Now())
	assert.Error(t, e)
}
