package reporterupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryTasks struct {
	tasks   map[string]Task
	changes []string
	fail    string
}

func (p *memoryTasks) Snapshot(n string) (Task, error) {
	if v, ok := p.tasks[n]; ok {
		return v, nil
	}
	return Task{Name: n}, nil
}
func (p *memoryTasks) Disable(n string) error {
	p.changes = append(p.changes, "disable:"+n)
	return nil
}
func (p *memoryTasks) Install(t Task) error {
	if t.Name == p.fail && strings.Contains(t.Definition, "release-2") {
		return errors.New("injected failure")
	}
	p.tasks[t.Name] = t
	p.changes = append(p.changes, "install:"+t.Name)
	return nil
}
func (p *memoryTasks) Start(n string) error {
	if _, ok := p.tasks[n]; !ok {
		return os.ErrNotExist
	}
	p.changes = append(p.changes, "start:"+n)
	return nil
}
func (p *memoryTasks) Delete(n string) error        { delete(p.tasks, n); return nil }
func (p *memoryTasks) Running(string) (bool, error) { return false, nil }
func (p *memoryTasks) Definition(n, b string, args []string, minutes int, keep bool) (Task, error) {
	return Task{Name: n, Definition: b + " " + strings.Join(args, " "), Exists: true}, nil
}
func (p *memoryTasks) Detach(string, string, string) error { return nil }
func (p *memoryTasks) Retarget(t Task, binary string, args []string) (Task, error) {
	return p.Definition(t.Name, binary, args, 5, false)
}

type updateFixture struct {
	m        *Manager
	tasks    *memoryTasks
	catalog  Catalog
	envelope []byte
	key      ed25519.PrivateKey
	keys     TrustKeys
	requests int
	binary   []byte
	server   *httptest.Server
}

func newUpdateFixture(t *testing.T) *updateFixture {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, e)
	f := &updateFixture{key: key, keys: TrustKeys{"key-1": base64.StdEncoding.EncodeToString(pub)}, binary: []byte("verified candidate fixture")}
	f.catalog = Catalog{SchemaVersion: 1, Sequence: 2, IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(), ReleaseID: "release-2", Version: "1.1.0", Commit: "commit-2", Protocol: Range{1, 1}, Config: Range{1, 1}, State: Range{2, 2}, Queue: Range{1, 1}, RolloutPercent: 100, RolloutSeed: "stable"}
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
		name, _ := TargetName(target)
		f.catalog.Targets = append(f.catalog.Targets, Artifact{Target: target, Path: "/api/external-usage/reporter/releases/release-2/" + target + "/" + name, Size: int64(len(f.binary)), SHA256: Digest(f.binary)})
	}
	f.envelope, e = Sign(f.catalog, "key-1", key)
	require.NoError(t, e)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests++
		if strings.HasSuffix(r.URL.Path, "/stable") {
			_, _ = w.Write(f.envelope)
			return
		}
		_, _ = w.Write(f.binary)
	}))
	t.Cleanup(f.server.Close)
	root := t.TempDir()
	f.tasks = &memoryTasks{tasks: map[string]Task{}}
	f.m = &Manager{Root: root, Target: "darwin-arm64", HTTP: &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, platform: f.tasks, now: time.Now, probe: func(context.Context, string, []Profile) error { return nil }, health: func(context.Context, Profile, string, string) error { return nil }, stop: func(context.Context, Profile) error { return nil }}
	previous := filepath.Join(root, "releases", "release-1", "darwin-arm64", "usage-reporter")
	require.NoError(t, SecureDir(filepath.Dir(previous)))
	require.NoError(t, os.WriteFile(previous, []byte("old binary"), 0o700))
	i := Installation{Schema: 1, ID: "local-install", Server: f.server.URL, AutoUpdate: true, Keys: f.keys, Current: Release{ID: "release-1", Version: "1.0.0", Binary: previous, SHA256: Digest([]byte("old binary"))}}
	for _, source := range []string{"codex", "claude_code"} {
		config := filepath.Join(root, source+".json")
		require.NoError(t, os.WriteFile(config, []byte("original private config"), 0o600))
		p := Profile{Source: source, Config: config, TaskName: "task-" + source, Streaming: source == "claude_code"}
		i.Profiles = append(i.Profiles, p)
		f.tasks.tasks[p.TaskName] = Task{Name: p.TaskName, Definition: "old " + source, Exists: true}
	}
	f.tasks.tasks[updaterTask] = Task{Name: updaterTask, Definition: "old updater", Exists: true}
	require.NoError(t, Save(filepath.Join(root, "installation.json"), i))
	return f
}

func TestSignedUpgradePreservesProfilesAndSwitchesUpdater(t *testing.T) {
	f := newUpdateFixture(t)
	id, e := f.m.CheckAndPrepare(context.Background(), false)
	require.NoError(t, e)
	require.NotEmpty(t, id)
	assert.NotContains(t, strings.Join(f.tasks.changes, " "), "disable:", "preparation must not stop collectors")
	require.NoError(t, f.m.Apply(context.Background(), id))
	i, s, e := f.m.read()
	require.NoError(t, e)
	assert.Equal(t, "release-2", i.Current.ID)
	assert.Equal(t, "committed", s.Phase)
	assert.Contains(t, f.tasks.tasks[updaterTask].Definition, "release-2")
	for _, p := range i.Profiles {
		data, e := os.ReadFile(p.Config)
		require.NoError(t, e)
		assert.Equal(t, "original private config", string(data))
		assert.Contains(t, f.tasks.tasks[p.TaskName].Definition, "release-2")
	}
	_, exists := f.tasks.tasks[recoveryTask]
	assert.False(t, exists)
}
func TestPartialSwitchRestoresAllTasksAndQuarantinesCandidate(t *testing.T) {
	f := newUpdateFixture(t)
	id, e := f.m.CheckAndPrepare(context.Background(), false)
	require.NoError(t, e)
	f.tasks.fail = "task-claude_code"
	require.Error(t, f.m.Apply(context.Background(), id))
	i, s, e := f.m.read()
	require.NoError(t, e)
	assert.Equal(t, "release-1", i.Current.ID)
	assert.Equal(t, "rolled_back", s.Phase)
	assert.Equal(t, "release-2", s.Quarantined)
	assert.Equal(t, "old codex", f.tasks.tasks["task-codex"].Definition)
	assert.Equal(t, "old claude_code", f.tasks.tasks["task-claude_code"].Definition)
	assert.Equal(t, "old updater", f.tasks.tasks[updaterTask].Definition)
}
func TestRecoveryAfterRestartIsIdempotentAndRunsWhenUpdatesDisabled(t *testing.T) {
	for _, stage := range []string{"prepared", "switching", "health_check", "metadata_written", "committed"} {
		t.Run(stage, func(t *testing.T) {
			f := newUpdateFixture(t)
			_, e := f.m.CheckAndPrepare(context.Background(), false)
			require.NoError(t, e)
			tx, e := f.m.readTransaction()
			require.NoError(t, e)
			tx.CreatedAt = time.Now().Add(-10 * time.Minute).Unix()
			tx.Phase = stage
			if stage == "metadata_written" {
				tx.Phase = "health_check"
			}
			require.NoError(t, f.m.saveTransaction(tx))
			i, s, e := f.m.read()
			require.NoError(t, e)
			i.AutoUpdate = false
			if stage != "prepared" {
				f.tasks.tasks["task-codex"] = Task{Name: "task-codex", Definition: "partly switched", Exists: true}
			}
			if stage == "metadata_written" || stage == "committed" {
				i.Current = tx.Candidate
			}
			if stage == "committed" {
				s.Phase = "committed"
				require.NoError(t, Save(filepath.Join(f.m.Root, "update-state.json"), s))
			}
			require.NoError(t, Save(filepath.Join(f.m.Root, "installation.json"), i))
			restarted := *f.m
			require.NoError(t, restarted.Recover(context.Background()))
			require.NoError(t, restarted.Recover(context.Background()))
			i, s, e = f.m.read()
			require.NoError(t, e)
			assert.False(t, i.AutoUpdate)
			if stage == "committed" {
				assert.Equal(t, "release-2", i.Current.ID)
				assert.Equal(t, "committed", s.Phase)
			} else {
				assert.Equal(t, "release-1", i.Current.ID)
				assert.Equal(t, "old codex", f.tasks.tasks["task-codex"].Definition)
			}
		})
	}
}

func TestInvalidCandidateNeverStopsCollectors(t *testing.T) {
	for _, kind := range []string{"signature", "checksum", "replay", "withdrawn"} {
		t.Run(kind, func(t *testing.T) {
			f := newUpdateFixture(t)
			switch kind {
			case "signature":
				f.envelope[len(f.envelope)-4] ^= 1
			case "checksum":
				f.binary = []byte("corrupted")
			case "replay":
				require.NoError(t, f.m.saveState(UpdateState{Sequence: 3, CatalogHash: "newer"}))
			case "withdrawn":
				f.catalog.Paused = true
				var e error
				f.envelope, e = Sign(f.catalog, "key-1", f.key)
				require.NoError(t, e)
			}
			id, e := f.m.CheckAndPrepare(context.Background(), true)
			if kind != "withdrawn" {
				assert.Error(t, e)
			} else {
				require.NoError(t, e)
			}
			assert.Empty(t, id)
			assert.Empty(t, f.tasks.changes)
		})
	}
}
func TestCheckScheduleAndDisabledFlagAvoidNetwork(t *testing.T) {
	f := newUpdateFixture(t)
	require.NoError(t, f.m.saveState(UpdateState{NextCheck: time.Now().Add(time.Hour).Unix()}))
	_, e := f.m.CheckAndPrepare(context.Background(), false)
	require.NoError(t, e)
	assert.Zero(t, f.requests)
	require.NoError(t, f.m.SetAutomatic(false))
	require.NoError(t, f.m.saveState(UpdateState{}))
	_, e = f.m.CheckAndPrepare(context.Background(), false)
	require.NoError(t, e)
	assert.Zero(t, f.requests)
}
func TestOriginAndArtifactTrustRejectUnexpectedPaths(t *testing.T) {
	for _, bad := range []string{"https://user:password@gateway", "file:///tmp/a", "https://gateway/api", "https://gateway?token=secret"} {
		_, e := Origin(bad)
		assert.Error(t, e)
	}
	for _, bad := range []string{"//other/a", "/api/external-usage/reporter/../secrets", "/api/external-usage/reporter/%2e%2e/x", "/api/external-usage/reporter/a?token=x"} {
		_, e := ResourceURL("https://gateway", bad)
		assert.Error(t, e)
	}
	assert.True(t, Newer("1.10.0", "1.9.9"))
	assert.False(t, Newer("1.0.0", "1.0.0"))
	assert.False(t, Newer("01.0.0", "1.0.0"))
}

func TestExplicitEnrollmentWorksOutsideAutomaticRollout(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "manual-install"}[manual], func(t *testing.T) {
			f := newUpdateFixture(t)
			id, e := f.m.CheckAndPrepare(context.Background(), true)
			require.NoError(t, e)
			tx, e := f.m.readTransaction()
			require.NoError(t, e)
			tx.Enrollment = manual
			require.NoError(t, f.m.saveTransaction(tx))
			f.catalog.Sequence++
			f.catalog.RolloutPercent = 0
			f.envelope, e = Sign(f.catalog, "key-1", f.key)
			require.NoError(t, e)
			e = f.m.Apply(context.Background(), id)
			if manual {
				require.NoError(t, e)
			} else {
				require.Error(t, e)
				assert.NotContains(t, strings.Join(f.tasks.changes, " "), "disable:")
			}
		})
	}
}
