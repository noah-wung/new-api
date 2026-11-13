package reporterupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
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
	port, e := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, e)
	listen := port.Addr().String()
	require.NoError(t, port.Close())
	claudeConfig := filepath.Join(root, "claude", "config.json")
	claudeState := filepath.Join(root, "claude", "state.json")
	require.NoError(t, SecureDir(filepath.Dir(claudeConfig)))
	claude := usagereporter.Config{Server: server.URL, Source: usagereporter.SourceClaudeCode, Credential: "fixture-claude", StatePath: claudeState, AcceptWindowDays: 90, ClaudeListen: listen, ClaudeReceiverToken: strings.Repeat("a", 64), ClaudeAutoRoute: true, GatewayURLs: []string{"https://gateway.example"}}
	require.NoError(t, usagereporter.SaveConfig(claudeConfig, claude))
	claudeProfile := Profile{Source: "claude_code", Config: claudeConfig, TaskName: "test-claude", Streaming: true}
	old := Release{ID: "native-old", Version: "1.0.0", Binary: filepath.Join(root, "releases", "native-old", m.Target, "usage-reporter"), SHA256: Digest(oldBytes)}
	task, e := p.Definition(profile.TaskName, old.Binary, workerArguments(profile, "old-transaction"), 5, false)
	require.NoError(t, e)
	require.NoError(t, p.Install(task))
	claudeTask, e := p.Definition(claudeProfile.TaskName, old.Binary, workerArguments(claudeProfile, "old-transaction"), 0, true)
	require.NoError(t, e)
	require.NoError(t, p.Install(claudeTask))
	u, e := p.Definition(updaterTask, old.Binary, []string{"--update-check", "--runtime", root}, 15, false)
	require.NoError(t, e)
	require.NoError(t, p.Install(u))
	require.NoError(t, Save(filepath.Join(root, "installation.json"), Installation{Schema: 1, ID: "native-install", Server: server.URL, Keys: keys, AutoUpdate: true, Profiles: []Profile{profile, claudeProfile}, Current: old}))
	launcher, e := EnsureLauncher(root, old.Binary, Artifact{Target: m.Target, SHA256: Digest(oldBytes), Size: int64(len(oldBytes))})
	require.NoError(t, e)
	settings := filepath.Join(root, "settings.json")
	require.NoError(t, usagereporter.ConfigureClaudeTelemetry(claude, settings, launcher, usagereporter.ClaudeTelemetryOptions{RuntimeDir: root}))
	originalSettings, e := os.ReadFile(settings)
	require.NoError(t, e)
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
	afterSettings, e := os.ReadFile(settings)
	require.NoError(t, e)
	assert.Equal(t, originalSettings, afterSettings, "updates must not rewrite Claude settings")
	command := exec.Command(launcher, "--claude-otel-headers", "--runtime", root)
	command.Env = append(os.Environ(), "ANTHROPIC_BASE_URL=https://third.example/v1")
	headers, e := command.Output()
	require.NoError(t, e)
	var route map[string]string
	require.NoError(t, common.Unmarshal(headers, &route))
	assert.Equal(t, "https://third.example", route[usagereporter.ClaudeOriginHeader])
	// Exercise the real updated receiver, not only lifecycle health receipts.
	body := fmt.Sprintf(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"eventName":"claude_code.api_request","timeUnixNano":"%d","attributes":[{"key":"model","value":{"stringValue":"claude-fixture"}},{"key":"request_id","value":{"stringValue":"native-update-request"}},{"key":"input_tokens","value":{"intValue":"100"}},{"key":"output_tokens","value":{"intValue":"40"}}]}]}]}]}`, time.Now().UnixNano())
	request, e := http.NewRequest(http.MethodPost, "http://"+listen+"/v1/logs", bytes.NewBufferString(body))
	require.NoError(t, e)
	request.Header.Set("Authorization", "Bearer "+claude.ClaudeReceiverToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(usagereporter.ClaudeOriginHeader, route[usagereporter.ClaudeOriginHeader])
	response, e := http.DefaultClient.Do(request)
	require.NoError(t, e)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
	queue := filepath.Join(claudeState+".claude-queue", Digest([]byte(server.URL+"\x00fixture-claude")))
	files, e := os.ReadDir(queue)
	require.NoError(t, e)
	assert.Len(t, files, 1, "updated Claude process must persist the external event")
}

// This opt-in test waits for an actual five-minute LaunchAgent interval. Its
// source, server, runtime, credentials and task labels are all isolated.
func TestNativeDarwinZCodeSignedUpdateAndScheduledReport(t *testing.T) {
	if os.Getenv("NEW_API_TEST_NATIVE_ZCODE") != "1" {
		t.Skip("opt-in native ZCode scheduling integration")
	}
	root := t.TempDir()
	m, err := NewManager(filepath.Join(root, "runtime"))
	require.NoError(t, err)
	p := &isolatedDarwinTasks{base: m.platform.(*nativePlatform), prefix: "pro.newapi.test." + randomID()[:8] + ".", names: map[string]bool{}}
	p.base.agents = filepath.Join(root, "agents")
	m.platform = p
	t.Cleanup(func() {
		for name := range p.names {
			_ = p.base.Delete(name)
		}
	})
	project, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	build := func(id, version string) []byte {
		path := filepath.Join(m.Root, "releases", id, m.Target, "usage-reporter")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		cmd := exec.Command("go", "build", "-ldflags", "-X github.com/QuantumNous/new-api/pkg/reporterupdate.Version="+version+" -X github.com/QuantumNous/new-api/pkg/reporterupdate.BuildReleaseID="+id, "-o", path, "./cmd/usage-reporter")
		cmd.Dir = project
		out, e := cmd.CombinedOutput()
		require.NoError(t, e, string(out))
		data, e := os.ReadFile(path)
		require.NoError(t, e)
		return data
	}
	oldBytes := build("zcode-old", "1.0.0")
	newBytes := build("zcode-new", "1.1.0")
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	keys := TrustKeys{"test-key": base64.StdEncoding.EncodeToString(pub)}
	catalog := Catalog{SchemaVersion: 1, Sequence: 1, IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(20 * time.Minute).Unix(), ReleaseID: "zcode-new", Version: "1.1.0", Commit: "development", Protocol: Range{1, 1}, Config: Range{1, 1}, State: Range{2, 2}, Queue: Range{1, 1}, RolloutPercent: 100, RolloutSeed: "test"}
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
		name, _ := TargetName(target)
		catalog.Targets = append(catalog.Targets, Artifact{Target: target, Path: "/api/external-usage/reporter/releases/zcode-new/" + target + "/" + name, SHA256: Digest(newBytes), Size: int64(len(newBytes))})
	}
	envelope, err := Sign(catalog, "test-key", key)
	require.NoError(t, err)
	reports := make(chan usagereporter.ReportPayload, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/stable"):
			_, _ = w.Write(envelope)
		case strings.Contains(r.URL.Path, "/releases/"):
			_, _ = w.Write(newBytes)
		case r.URL.Path == "/api/external-usage/report":
			var payload usagereporter.ReportPayload
			if common.DecodeJson(r.Body, &payload) != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			reports <- payload
			var ids []string
			for _, event := range payload.Events {
				ids = append(ids, event.EventID)
			}
			body, _ := common.Marshal(map[string]any{"success": true, "data": usagereporter.ReportAcknowledgement{AcceptedEventIDs: ids}})
			_, _ = w.Write(body)
		default:
			_, _ = w.Write([]byte(`{"success":true,"data":{"external_usage_accept_window_days":90}}`))
		}
	}))
	defer server.Close()
	dbPath := filepath.Join(root, "zcode.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	insert := func(id, provider string, input int) {
		now := time.Now().UnixMilli()
		_, e := db.Exec(`INSERT INTO model_usage VALUES (?, 'session', ?, 'fixture-model', 'completed', ?, ?, ?, 5, 0, 0, 0, ?)`, id, provider, now, now, input, input+5)
		require.NoError(t, e)
	}
	insert("first", "external", 10)
	insert("gateway", "gateway", 20)
	insert("unknown", "unknown", 30)
	providerPath := filepath.Join(root, "providers.json")
	require.NoError(t, os.WriteFile(providerPath, []byte(`{"provider":{"external":{"options":{"baseURL":"https://external.example"}},"gateway":{"options":{"baseURL":"https://token.einyun.com"}}}}`), 0o600))
	config := filepath.Join(root, "zcode", "config.json")
	require.NoError(t, SecureDir(filepath.Dir(config)))
	cfg := usagereporter.Config{Server: server.URL, Source: usagereporter.SourceZCode, Credential: "fixture", ZCodeDBPath: dbPath, ZCodeProviderConfigPath: providerPath, ZCodeGatewayHosts: []string{"token.einyun.com"}, StatePath: filepath.Join(root, "zcode", "state.json"), AcceptWindowDays: 90}
	require.NoError(t, usagereporter.SaveConfig(config, cfg))
	profile := Profile{Source: usagereporter.SourceZCode, Config: config, TaskName: "test-zcode"}
	old := Release{ID: "zcode-old", Version: "1.0.0", Binary: filepath.Join(m.Root, "releases", "zcode-old", m.Target, "usage-reporter"), SHA256: Digest(oldBytes)}
	task, err := p.Definition(profile.TaskName, old.Binary, workerArguments(profile, "old"), 5, false)
	require.NoError(t, err)
	require.NoError(t, p.Install(task))
	updater, err := p.Definition(updaterTask, old.Binary, []string{"--update-check", "--runtime", m.Root}, 15, false)
	require.NoError(t, err)
	require.NoError(t, p.Install(updater))
	require.NoError(t, Save(filepath.Join(m.Root, "installation.json"), Installation{Schema: 1, ID: "test-install", Server: server.URL, Keys: keys, AutoUpdate: true, Profiles: []Profile{profile}, Current: old}))
	id, err := m.CheckAndPrepare(context.Background(), true)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	require.NoError(t, m.Apply(context.Background(), id))
	select {
	case report := <-reports:
		require.Equal(t, usagereporter.SourceZCode, report.Source)
		require.Len(t, report.Events, 1)
		require.Equal(t, 10, report.Events[0].InputTokens)
	case <-time.After(15 * time.Second):
		t.Fatal("updated ZCode task did not report the initial external event")
	}
	insert("second", "external", 40)
	select {
	case report := <-reports:
		require.Len(t, report.Events, 1)
		require.Equal(t, 40, report.Events[0].InputTokens, "scheduled run must send only the new external event")
	case <-time.After(6 * time.Minute):
		t.Fatal("five-minute LaunchAgent interval did not report the new event")
	}
	current, state, err := m.read()
	require.NoError(t, err)
	require.Equal(t, "zcode-new", current.Current.ID)
	require.Equal(t, "committed", state.Phase)
	actual, err := usagereporter.LoadConfig(config)
	require.NoError(t, err)
	require.Equal(t, cfg.Credential, actual.Credential)
	require.Equal(t, cfg.StatePath, actual.StatePath)
}
