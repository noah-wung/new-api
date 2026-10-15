package reporterupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each task is mapped to a unique test label; no installed user collector is touched.
type isolatedDarwinTasks struct {
	base   *nativePlatform
	prefix string
	names  map[string]bool
}

func (p *isolatedDarwinTasks) name(n string) string {
	if strings.HasPrefix(n, p.prefix) {
		return n
	}
	return p.prefix + Digest([]byte(n))[:16]
}
func (p *isolatedDarwinTasks) Snapshot(n string) (Task, error) {
	task, e := p.base.Snapshot(p.name(n))
	task.Name = n
	return task, e
}
func (p *isolatedDarwinTasks) Disable(n string) error { return p.base.Disable(p.name(n)) }
func (p *isolatedDarwinTasks) Install(t Task) error {
	t.Name = p.name(t.Name)
	p.names[t.Name] = true
	return p.base.Install(t)
}
func (p *isolatedDarwinTasks) Start(n string) error           { return p.base.Start(p.name(n)) }
func (p *isolatedDarwinTasks) Delete(n string) error          { return p.base.Delete(p.name(n)) }
func (p *isolatedDarwinTasks) Running(n string) (bool, error) { return p.base.Running(p.name(n)) }
func (p *isolatedDarwinTasks) Definition(n, b string, args []string, minutes int, keep bool) (Task, error) {
	return p.base.Definition(p.name(n), b, args, minutes, keep)
}
func (p *isolatedDarwinTasks) Detach(string, string, string) error { return nil }
func (p *isolatedDarwinTasks) Retarget(t Task, binary string, args []string) (Task, error) {
	return p.base.Retarget(t, binary, args)
}

func TestNativeDarwinSignedUpdateKeepsStateAndCompletesCollectorRun(t *testing.T) {
	if os.Getenv("NEW_API_TEST_NATIVE_UPDATER") != "1" {
		t.Skip("opt-in native user LaunchAgent integration")
	}
	root := t.TempDir()
	m, e := NewManager(root)
	require.NoError(t, e)
	root = m.Root
	p := &isolatedDarwinTasks{base: m.platform.(*nativePlatform), prefix: "pro.newapi.test." + randomID()[:8] + ".", names: map[string]bool{}}
	p.base.agents = filepath.Join(root, "agents")
	m.platform = p
	t.Cleanup(func() {
		for name := range p.names {
			_ = p.base.Delete(name)
		}
	})
	project, e := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, e)
	build := func(id, version string) []byte {
		path := filepath.Join(root, "releases", id, runtime.GOOS+"-"+runtime.GOARCH, "usage-reporter")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		cmd := exec.Command("go", "build", "-ldflags", "-X github.com/QuantumNous/new-api/pkg/reporterupdate.Version="+version+" -X github.com/QuantumNous/new-api/pkg/reporterupdate.BuildReleaseID="+id, "-o", path, "./cmd/usage-reporter")
		cmd.Dir = project
		out, e := cmd.CombinedOutput()
		require.NoError(t, e, string(out))
		data, e := os.ReadFile(path)
		require.NoError(t, e)
		return data
	}
	oldBytes := build("native-old", "1.0.0")
	newBytes := build("native-new", "1.1.0")
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, e)
	keys := TrustKeys{"native": base64.StdEncoding.EncodeToString(pub)}
	c := Catalog{SchemaVersion: 1, Sequence: 1, IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(), ReleaseID: "native-new", Version: "1.1.0", Commit: "development", Protocol: Range{1, 1}, Config: Range{1, 1}, State: Range{2, 2}, Queue: Range{1, 1}, RolloutPercent: 100, RolloutSeed: "native"}
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
		name, _ := TargetName(target)
		c.Targets = append(c.Targets, Artifact{Target: target, Path: "/api/external-usage/reporter/releases/native-new/" + target + "/" + name, SHA256: Digest(newBytes), Size: int64(len(newBytes))})
	}
	envelope, e := Sign(c, "native", key)
	require.NoError(t, e)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stable") {
			_, _ = w.Write(envelope)
			return
		}
		if strings.Contains(r.URL.Path, "/releases/") {
			_, _ = w.Write(newBytes)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"external_usage_accept_window_days":90}}`))
	}))
	defer server.Close()
	config := filepath.Join(root, "config.json")
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.Mkdir(sessions, 0o700))
	state := filepath.Join(root, "state.json")
	require.NoError(t, Save(config, map[string]any{"server": server.URL, "source": "codex", "credential": "fixture-credential", "codex_dir": sessions, "state_path": state, "accept_window_days": 90, "gateway_urls": []string{"https://gateway.example"}}))
	profile := Profile{Source: "codex", Config: config, TaskName: "test-codex"}
	old := Release{ID: "native-old", Version: "1.0.0", Binary: filepath.Join(root, "releases", "native-old", m.Target, "usage-reporter"), SHA256: Digest(oldBytes)}
	task, e := p.Definition(profile.TaskName, old.Binary, workerArguments(profile, "old-transaction"), 5, false)
	require.NoError(t, e)
	require.NoError(t, p.Install(task))
	u, e := p.Definition(updaterTask, old.Binary, []string{"--update-check", "--runtime", root}, 15, false)
	require.NoError(t, e)
	require.NoError(t, p.Install(u))
	require.NoError(t, Save(filepath.Join(root, "installation.json"), Installation{Schema: 1, ID: "native-install", Server: server.URL, Keys: keys, AutoUpdate: true, Profiles: []Profile{profile}, Current: old}))
	id, e := m.CheckAndPrepare(context.Background(), true)
	require.NoError(t, e)
	require.NotEmpty(t, id)
	require.NoError(t, m.Apply(context.Background(), id))
	i, s, e := m.read()
	require.NoError(t, e)
	assert.Equal(t, "native-new", i.Current.ID)
	assert.Equal(t, "committed", s.Phase)
	var receipt Health
	require.NoError(t, Load(config+".health.json", &receipt))
	assert.Equal(t, id, receipt.Transaction)
	assert.Equal(t, "native-new", receipt.ReleaseID)
	var cfg map[string]any
	require.NoError(t, Load(config, &cfg))
	assert.Equal(t, "fixture-credential", cfg["credential"])
	assert.Equal(t, state, cfg["state_path"])
}
