package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/QuantumNous/new-api/pkg/usagereporter"
	"github.com/stretchr/testify/require"
)

// Stop at the exchange response so this test never changes real OS task names.
// The signed installer checks and the ZCode preflight are otherwise real.
func TestSignedZCodeAddReachesExchangeWithoutChangingExistingCollector(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dbPath := filepath.Join(home, "zcode.sqlite")
	t.Setenv("ZCODE_SESSION_DB_PATH", dbPath)
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER, cache_read_input_tokens INTEGER, computed_total_tokens INTEGER)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	requests := make(chan string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		http.Error(w, "fixture rejection", http.StatusForbidden)
	}))
	defer server.Close()
	previousTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	root := filepath.Join(home, ".new-api-usage-reporter")
	codexPath := filepath.Join(root, "config.json")
	codexState := filepath.Join(root, "state.json")
	require.NoError(t, usagereporter.SaveConfig(codexPath, usagereporter.Config{Server: server.URL, Source: usagereporter.SourceCodex, Credential: "existing-codex", StatePath: codexState}))
	require.NoError(t, os.WriteFile(codexState, []byte(`{"fixture":"preserve"}`), 0o600))
	beforeConfig, err := os.ReadFile(codexPath)
	require.NoError(t, err)
	beforeState, err := os.ReadFile(codexState)
	require.NoError(t, err)
	executable, err := os.Executable()
	require.NoError(t, err)
	binary, err := os.ReadFile(executable)
	require.NoError(t, err)
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	keys := reporterupdate.TrustKeys{"fixture": base64.StdEncoding.EncodeToString(pub)}
	catalog := reporterupdate.Catalog{SchemaVersion: 1, Sequence: 1, IssuedAt: time.Now().Add(-time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix(), ReleaseID: reporterupdate.BuildReleaseID, Version: reporterupdate.Version, Commit: reporterupdate.Commit, Protocol: reporterupdate.Range{Min: 1, Max: 1}, Config: reporterupdate.Range{Min: 1, Max: 1}, State: reporterupdate.Range{Min: 2, Max: 2}, Queue: reporterupdate.Range{Min: 1, Max: 1}, RolloutPercent: 100, RolloutSeed: "fixture"}
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
		name, e := reporterupdate.TargetName(target)
		require.NoError(t, e)
		catalog.Targets = append(catalog.Targets, reporterupdate.Artifact{Target: target, Path: "/api/external-usage/reporter/releases/" + catalog.ReleaseID + "/" + target + "/" + name, Size: int64(len(binary)), SHA256: reporterupdate.Digest(binary)})
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("managed release targets macOS and Windows")
	}
	envelope, err := reporterupdate.Sign(catalog, "fixture", private)
	require.NoError(t, err)
	keysPath := filepath.Join(home, "keys.json")
	catalogPath := filepath.Join(home, "catalog.json")
	require.NoError(t, reporterupdate.Save(keysPath, keys))
	badSignature := append([]byte(nil), envelope...)
	badSignature[len(badSignature)-4] ^= 1
	require.NoError(t, os.WriteFile(catalogPath, badSignature, 0o600))
	require.Error(t, installClients(server.URL, "", "", "fixture-token", keysPath, catalogPath, true))
	require.Empty(t, requests, "invalid signatures must fail before token exchange")
	badCatalog := catalog
	badCatalog.Targets = append([]reporterupdate.Artifact(nil), catalog.Targets...)
	for i := range badCatalog.Targets {
		if badCatalog.Targets[i].Target == runtime.GOOS+"-"+runtime.GOARCH {
			badCatalog.Targets[i].SHA256 = reporterupdate.Digest([]byte("different binary"))
		}
	}
	badDigest, err := reporterupdate.Sign(badCatalog, "fixture", private)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catalogPath, badDigest, 0o600))
	require.Error(t, installClients(server.URL, "", "", "fixture-token", keysPath, catalogPath, true))
	require.Empty(t, requests, "wrong binary digest must fail before token exchange")
	require.NoError(t, os.WriteFile(catalogPath, envelope, 0o600))
	err = installClients(server.URL, "", "", "fixture-token", keysPath, catalogPath, true)
	require.ErrorContains(t, err, "installer exchange rejected")
	select {
	case path := <-requests:
		require.Equal(t, "/api/external-usage/self/zcode-installer/exchange", path)
	case <-time.After(time.Second):
		t.Fatal("signed ZCode installer did not reach exchange")
	}
	afterConfig, err := os.ReadFile(codexPath)
	require.NoError(t, err)
	afterState, err := os.ReadFile(codexState)
	require.NoError(t, err)
	require.Equal(t, beforeConfig, afterConfig)
	require.Equal(t, beforeState, afterState)
	_, err = os.Stat(filepath.Join(root, "zcode", "config.json"))
	require.True(t, os.IsNotExist(err))
}
