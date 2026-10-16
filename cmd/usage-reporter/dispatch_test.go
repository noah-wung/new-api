package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeHeaderDispatchFollowsCommittedVersionAndRollback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Unix executable fixture for a managed target")
	}
	root := t.TempDir()
	target := runtime.GOOS + "-" + runtime.GOARCH
	name, err := reporterupdate.TargetName(target)
	require.NoError(t, err)
	installation := reporterupdate.Installation{Schema: 1, ID: "fixture-install", Server: "https://gateway.example", Keys: reporterupdate.TrustKeys{"fixture": "public-key"}, Profiles: []reporterupdate.Profile{{Source: "claude_code", Config: filepath.Join(root, "config.json"), TaskName: "fixture-task", Streaming: true}}}
	releases := map[string]reporterupdate.Release{}
	for _, id := range []string{"previous", "candidate"} {
		path := filepath.Join(root, "releases", id, target, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		content := []byte("#!/bin/sh\nprintf '%s\\n' '{\"X-New-Api-Upstream-Origin\":\"https://" + id + ".example\"}'\n")
		require.NoError(t, os.WriteFile(path, content, 0o700))
		releases[id] = reporterupdate.Release{ID: id, Version: "1.0.3", Binary: path, SHA256: reporterupdate.Digest(content)}
	}
	for _, id := range []string{"previous", "candidate", "previous"} {
		installation.Current = releases[id]
		require.NoError(t, reporterupdate.Save(filepath.Join(root, "installation.json"), installation))
		output := runReporterCommand(t, "--claude-otel-headers", "--runtime", root)
		assert.JSONEq(t, `{"X-New-Api-Upstream-Origin":"https://`+id+`.example"}`, output)
	}
}

func TestLegacyInstallerCannotReplaceManagedCollector(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".new-api-usage-reporter", "runtime")
	config := filepath.Join(home, ".new-api-usage-reporter", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(config), 0o700))
	original := []byte("private configuration must remain unchanged")
	require.NoError(t, os.WriteFile(config, original, 0o600))
	installation := reporterupdate.Installation{Schema: 1, ID: "managed-install", Server: "https://gateway.example", Keys: reporterupdate.TrustKeys{"fixture": "public-key"}, Current: reporterupdate.Release{ID: "current", Version: "1.0.3", Binary: filepath.Join(root, "releases", "current", "usage-reporter")}, Profiles: []reporterupdate.Profile{{Source: "codex", Config: config, TaskName: "fixture-task"}}}
	require.NoError(t, reporterupdate.Save(filepath.Join(root, "installation.json"), installation))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	args := []string{"-test.run=^TestUsageReporterSubprocess$", "--", "--install-codex", "--server", server.URL, "--install-token", "fixture-token", "--config", config}
	command := exec.Command(os.Args[0], args...)
	command.Env = append(os.Environ(), "NEW_API_USAGE_REPORTER_TEST_PROCESS=1", "HOME="+home, "USERPROFILE="+home)
	output, err := command.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "this collector is managed")
	assert.Zero(t, requests.Load(), "refuse before exchanging a token or rewriting a configuration")
	after, err := os.ReadFile(config)
	require.NoError(t, err)
	assert.Equal(t, original, after)
}
