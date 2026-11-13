package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
	"github.com/stretchr/testify/require"
)

// Run the actual signed installer and LaunchAgent lifecycle with test-only
// label, provider-path and localhost-HTTP substitutions via Go's build overlay.
func TestNativeDarwinSignedZCodeAddToCodex(t *testing.T) {
	if os.Getenv("NEW_API_TEST_NATIVE_INSTALL") != "1" {
		t.Skip("opt-in isolated native signed installation")
	}
	large := os.Getenv("NEW_API_TEST_NATIVE_ZCODE_RECOVERY") == "1"
	standalone := os.Getenv("NEW_API_TEST_NATIVE_ZCODE_STANDALONE") == "1"
	root := t.TempDir()
	project, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	var nonce [8]byte
	_, err = rand.Read(nonce[:])
	require.NoError(t, err)
	prefix := fmt.Sprintf("pro.newapi.test.%x", nonce[:])
	providerPath := filepath.Join(root, "providers.json")
	replacements := map[string]string{}
	overlay := func(relative string, edit func(string) string) {
		original := filepath.Join(project, relative)
		data, e := os.ReadFile(original)
		require.NoError(t, e)
		changed := edit(string(data))
		require.NotEqual(t, string(data), changed)
		path := filepath.Join(root, fmt.Sprintf("overlay-%d.go", len(replacements)))
		require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
		replacements[original] = path
	}
	overlay("cmd/usage-reporter/update.go", func(s string) string {
		require.Equal(t, 3, strings.Count(s, "pro.newapi.usage-reporter."))
		require.Equal(t, 1, strings.Count(s, `!strings.HasPrefix(server, "https://")`))
		s = strings.ReplaceAll(s, "pro.newapi.usage-reporter.", prefix+".")
		return strings.Replace(s, `!strings.HasPrefix(server, "https://")`, `!strings.HasPrefix(server, "http://")`, 1)
	})
	overlay("pkg/reporterupdate/transaction.go", func(s string) string {
		require.Equal(t, 3, strings.Count(s, "pro.newapi.usage-reporter."))
		return strings.ReplaceAll(s, "pro.newapi.usage-reporter.", prefix+".")
	})
	overlay("pkg/usagereporter/config.go", func(s string) string {
		old := `DefaultZCodeProviderConfig = "~/.zcode/v2/config.json"`
		require.Equal(t, 1, strings.Count(s, old))
		return strings.Replace(s, old, "DefaultZCodeProviderConfig = "+strconv.Quote(providerPath), 1)
	})
	jsonOverlay, err := common.Marshal(map[string]any{"Replace": replacements})
	require.NoError(t, err)
	overlayPath := filepath.Join(root, "overlay.json")
	require.NoError(t, os.WriteFile(overlayPath, jsonOverlay, 0o600))
	binaryPath := filepath.Join(root, "usage-reporter")
	build := exec.Command("go", "build", "-overlay", overlayPath, "-o", binaryPath, "./cmd/usage-reporter")
	build.Dir = project
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))
	binary, err := os.ReadFile(binaryPath)
	require.NoError(t, err)
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	keys := reporterupdate.TrustKeys{"fixture": base64.StdEncoding.EncodeToString(pub)}
	catalog := reporterupdate.Catalog{SchemaVersion: 1, Sequence: 1, IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(), ReleaseID: reporterupdate.BuildReleaseID, Version: reporterupdate.Version, Commit: reporterupdate.Commit, Protocol: reporterupdate.Range{Min: 1, Max: 1}, Config: reporterupdate.Range{Min: 1, Max: 1}, State: reporterupdate.Range{Min: 2, Max: 2}, Queue: reporterupdate.Range{Min: 1, Max: 1}, RolloutPercent: 100, RolloutSeed: "fixture"}
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
		name, e := reporterupdate.TargetName(target)
		require.NoError(t, e)
		catalog.Targets = append(catalog.Targets, reporterupdate.Artifact{Target: target, Path: "/api/external-usage/reporter/releases/" + catalog.ReleaseID + "/" + target + "/" + name, SHA256: reporterupdate.Digest(binary), Size: int64(len(binary))})
	}
	envelope, err := reporterupdate.Sign(catalog, "fixture", private)
	require.NoError(t, err)
	keysPath := filepath.Join(root, "keys.json")
	catalogPath := filepath.Join(root, "catalog.json")
	require.NoError(t, reporterupdate.Save(keysPath, keys))
	require.NoError(t, os.WriteFile(catalogPath, envelope, 0o600))
	reports := make(chan usagereporter.ReportPayload, 128)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/stable"):
			_, _ = w.Write(envelope)
		case strings.HasSuffix(r.URL.Path, "/codex-installer/exchange"):
			_, _ = w.Write([]byte(`{"success":true,"data":{"reporting_credential":"codex-fixture","source":"codex","accept_window_days":90}}`))
		case strings.HasSuffix(r.URL.Path, "/zcode-installer/exchange"):
			_, _ = w.Write([]byte(`{"success":true,"data":{"reporting_credential":"zcode-fixture","source":"zcode","accept_window_days":90}}`))
		case r.URL.Path == "/api/external-usage/report":
			var report usagereporter.ReportPayload
			if common.DecodeJson(r.Body, &report) != nil {
				http.Error(w, "invalid", http.StatusBadRequest)
				return
			}
			reports <- report
			var ids []string
			for _, event := range report.Events {
				ids = append(ids, event.EventID)
			}
			body, _ := common.Marshal(map[string]any{"success": true, "data": usagereporter.ReportAcknowledgement{AcceptedEventIDs: ids}})
			_, _ = w.Write(body)
		default:
			_, _ = w.Write([]byte(`{"success":true,"data":{"external_usage_accept_window_days":90}}`))
		}
	}))
	defer server.Close()
	t.Cleanup(func() {
		for _, name := range []string{"codex", "claude", "zcode", "updater", "recovery", "apply"} {
			_ = exec.Command("/bin/launchctl", "bootout", fmt.Sprintf("gui/%d/%s.%s", os.Getuid(), prefix, name)).Run()
		}
	})
	dbPath := filepath.Join(root, "zcode.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	now := time.Now().UnixMilli()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	if large {
		tx, e := db.Begin()
		require.NoError(t, e)
		for i := 0; i < 9773; i++ {
			provider, status := "direct", "completed"
			if i >= 9592 {
				provider = "unknown"
			}
			if i >= 9705 {
				status = "running"
			}
			_, e = tx.Exec(`INSERT INTO model_usage VALUES (?, 'session', ?, 'model', ?, ?, ?, 10, 5, 0, 0, 0, 15)`, fmt.Sprintf("%05d", i), provider, status, now, now)
			require.NoError(t, e)
		}
		require.NoError(t, tx.Commit())
	} else {
		_, err = db.Exec(`INSERT INTO model_usage VALUES ('fixture','session','direct','model','completed',?,?,10,5,0,0,0,15)`, now, now)
		require.NoError(t, err)
	}
	require.NoError(t, db.Close())
	require.NoError(t, os.WriteFile(providerPath, []byte(`{"provider":{"direct":{"options":{"baseURL":"https://external.example"}}}}`), 0o600))
	home := filepath.Join(root, "home")
	sessions := filepath.Join(root, "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	codexConfig := filepath.Join(home, ".new-api-usage-reporter", "config.json")
	codexState := filepath.Join(root, "codex-state.json")
	require.NoError(t, usagereporter.SaveConfig(codexConfig, usagereporter.Config{Server: server.URL, Source: usagereporter.SourceCodex, Credential: "codex-fixture", CodexDir: sessions, StatePath: codexState}))
	initialState := usagereporter.NewState()
	initialState.Acknowledged["fixture-id"] = 123
	require.NoError(t, initialState.Save(codexState))
	install := func(source string) {
		command := exec.Command(binaryPath, "--install-clients", "--server", server.URL, "--"+source+"-install-token", "fixture-token", "--trust-keys", keysPath, "--catalog", catalogPath, "--install-auto-update=true")
		command.Env = append(os.Environ(), "HOME="+home, "ZCODE_SESSION_DB_PATH="+dbPath)
		output, e := command.CombinedOutput()
		require.NoError(t, e, string(output))
		require.Contains(t, string(output), "Selected client collectors installed")
	}
	if !standalone {
		install("codex")
	}
	before, err := os.ReadFile(codexConfig)
	require.NoError(t, err)
	zcodeConfig := filepath.Join(home, ".new-api-usage-reporter", "zcode", "config.json")
	zcodeState := filepath.Join(home, ".new-api-usage-reporter", "zcode", "state.json")
	if large {
		cfg := usagereporter.Config{Server: server.URL, Source: usagereporter.SourceZCode, Credential: "zcode-fixture", StatePath: zcodeState, ZCodeDBPath: dbPath, ZCodeProviderConfigPath: providerPath, AcceptWindowDays: 90}
		require.NoError(t, usagereporter.SaveConfig(zcodeConfig, cfg))
		namespace := reporterupdate.Digest([]byte(server.URL + "\x00zcode-fixture"))
		queue := filepath.Join(zcodeState+".usage-queue", "zcode", namespace)
		require.NoError(t, os.MkdirAll(queue, 0o700))
		snapshots := make(map[string]any, 9705)
		for i := 0; i < 9705; i++ {
			id, provider := fmt.Sprintf("%05d", i), "direct"
			if i >= 9592 {
				provider = "unknown"
			}
			event := usagereporter.UsageEvent{EventID: reporterupdate.Digest([]byte("zcode:model_usage:v1\x00session\x00" + id)), SessionID: reporterupdate.Digest([]byte("zcode:session:v1\x00session")), OccurredAt: now / 1000, SourceModelName: "model", InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
			snapshots[event.EventID] = map[string]any{"event": event, "provider_id": provider, "started_at": now / 1000, "held": i < 9592, "submitted": false}
			if i < 8438 {
				record, e := common.Marshal(usagereporter.CollectedEvent{Event: event, RouteID: reporterupdate.Digest([]byte(provider)), Origin: "https://external.example"})
				require.NoError(t, e)
				require.NoError(t, os.WriteFile(filepath.Join(queue, event.EventID+".json"), record, 0o600))
			}
		}
		require.NoError(t, reporterupdate.Save(zcodeState, map[string]any{"version": 1, "namespace": namespace, "snapshots": snapshots, "invalid_count": 0}))
	}
	start := time.Now()
	install("zcode")
	t.Logf("ZCode signed installation completed in %s", time.Since(start))
	after, err := os.ReadFile(codexConfig)
	require.NoError(t, err)
	require.Equal(t, before, after)
	var installation reporterupdate.Installation
	require.NoError(t, reporterupdate.Load(filepath.Join(home, ".new-api-usage-reporter", "runtime", "installation.json"), &installation))
	profileCount := 2
	if standalone {
		profileCount = 1
	}
	require.Len(t, installation.Profiles, profileCount)
	require.Equal(t, runtime.GOOS+"-"+runtime.GOARCH, filepath.Base(filepath.Dir(installation.Current.Binary)))
	expected := 1
	if large {
		expected = 9592
	}
	seen := make(map[string]bool, expected)
	deadline := time.After(120 * time.Second)
	for len(seen) < expected {
		select {
		case report := <-reports:
			require.Equal(t, usagereporter.SourceZCode, report.Source)
			for _, event := range report.Events {
				require.Equal(t, 10, event.InputTokens)
				require.False(t, seen[event.EventID], "event was sent more than once")
				seen[event.EventID] = true
			}
		case <-deadline:
			t.Fatalf("ZCode LaunchAgent reported %d of %d fixture events", len(seen), expected)
		}
	}
	if large {
		require.Eventually(t, func() bool {
			var state struct {
				Version         int  `json:"version"`
				ArchiveRequired bool `json:"archive_required"`
				Snapshots       map[string]struct {
					Submitted bool `json:"submitted"`
				} `json:"snapshots"`
			}
			data, e := os.ReadFile(zcodeState)
			if e != nil || common.Unmarshal(data, &state) != nil {
				return false
			}
			if state.Version != 2 || !state.ArchiveRequired {
				return false
			}
			count := 0
			for _, snapshot := range state.Snapshots {
				if snapshot.Submitted {
					count++
				}
			}
			archivePath := zcodeState + ".archive.sqlite"
			if _, e := os.Stat(archivePath); e == nil {
				u := url.URL{Scheme: "file", Path: filepath.ToSlash(archivePath), RawQuery: "mode=ro"}
				archive, e := sql.Open("sqlite", u.String())
				if e != nil {
					return false
				}
				var archived int
				e = archive.QueryRow(`SELECT count(*) FROM snapshots`).Scan(&archived)
				_ = archive.Close()
				if e != nil {
					return false
				}
				count += archived
			}
			return count == expected
		}, 60*time.Second, 500*time.Millisecond, "all ACKs must be durable before reinstall")
	}
	state, err := usagereporter.LoadState(codexState)
	require.NoError(t, err)
	require.EqualValues(t, 123, state.Acknowledged["fixture-id"])
	zcodeBefore, err := os.ReadFile(zcodeConfig)
	require.NoError(t, err)
	install("zcode")
	zcodeAfter, err := os.ReadFile(zcodeConfig)
	require.NoError(t, err)
	require.Equal(t, zcodeBefore, zcodeAfter)
	select {
	case <-reports:
		t.Fatal("reinstallation resent acknowledged ZCode usage")
	case <-time.After(2 * time.Second):
	}
}
