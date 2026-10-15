package reporterupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusUsesSourceCredentialsAndDoesNotBlockOtherDeviceOnRejection(t *testing.T) {
	f := newUpdateFixture(t)
	var mu sync.Mutex
	counts := map[string]int{}
	headers := map[string]string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Source string `json:"source"`
		}
		if common.DecodeJson(r.Body, &body) != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		counts[body.Source]++
		headers[body.Source] = r.Header.Get("Authorization")
		mu.Unlock()
		if body.Source == "codex" {
			w.WriteHeader(403)
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	i, s, e := f.m.read()
	require.NoError(t, e)
	i.Server = server.URL
	for _, p := range i.Profiles {
		require.NoError(t, Save(p.Config, map[string]string{"server": server.URL, "source": p.Source, "credential": "credential-" + p.Source}))
	}
	require.NoError(t, Save(filepath.Join(f.m.Root, "installation.json"), i))
	s.Phase = "idle"
	require.NoError(t, Save(filepath.Join(f.m.Root, "update-state.json"), s))
	f.m.HTTP = server.Client()
	assert.Error(t, f.m.SendStatus(context.Background()))
	assert.Error(t, f.m.SendStatus(context.Background()))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, counts["codex"])
	assert.Equal(t, 1, counts["claude_code"])
	assert.Equal(t, "Bearer credential-codex", headers["codex"])
	assert.Equal(t, "Bearer credential-claude_code", headers["claude_code"])
}
