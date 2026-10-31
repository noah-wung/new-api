package usagereporter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoutePolicyCanonicalOriginsAndGatewayPrecedence(t *testing.T) {
	policy, err := NewRoutePolicy("https://token.example", []string{"http://[::1]:80", "https://alias.example"}, []string{"https://api.external.example", "https://token.example"})
	require.NoError(t, err)
	for _, value := range []string{"https://TOKEN.example:443/v1", "HTTPS://token.example:443/responses", "http://[::1]/v1", "http://[::1]:80/v1", "https://ALIAS.example./api"} {
		allowed, err := policy.Allows(value)
		require.NoError(t, err, value)
		assert.False(t, allowed, "gateway exclusion takes precedence: %s", value)
		origin, err := NormalizeUpstreamURL(value)
		require.NoError(t, err)
		again, err := NormalizeUpstreamURL(origin)
		require.NoError(t, err)
		assert.Equal(t, origin, again, "canonical origins must remain valid URLs")
	}
	allowed, err := policy.Allows("https://api.external.example/v1")
	require.NoError(t, err)
	assert.True(t, allowed)
	for _, value := range []string{"", "https://token.example:8443", "https://api.external.example.attacker.test", "https://key@api.external.example", "https://api.external.example?key=secret", "http://[::1]:65536"} {
		_, err := policy.Allows(value)
		assert.ErrorIs(t, err, ErrUnapprovedRoute, value)
	}
}

func TestReporterRechecksQueuedRouteAliasesUsingSharedPolicy(t *testing.T) {
	cfg := ReporterConfig{Source: "future_client", Server: "https://token.example", Credential: "fixture", StatePath: t.TempDir() + "/state.json"}
	reporter, err := NewReporter(cfg)
	require.NoError(t, err)
	event := reporterTestEvent("profile", "https://api.external.example")
	event.RouteID = sha256Hex("private-provider-label")
	require.NoError(t, reporter.Record(event))
	cfg.ExcludedRoutes = []string{event.RouteID}
	restarted, err := NewReporter(cfg)
	require.NoError(t, err)
	stats, err := restarted.FlushContext(t.Context())
	require.NoError(t, err, "newly excluded queued routes must be removed without an HTTP request")
	assert.Zero(t, stats.Sent)
	assert.NoFileExists(t, reporter.queueDir+"/"+event.Event.EventID+".json")
}

func TestRoutePolicyExcludesExactGatewayHostAcrossPorts(t *testing.T) {
	policy, err := NewRoutePolicy("https://report.example", nil, nil)
	require.NoError(t, err)
	require.NoError(t, policy.AddGatewayHosts([]string{" TOKEN.einyun.com. "}))
	for _, endpoint := range []string{"https://token.einyun.com", "https://TOKEN.EINYUN.COM:8443/v1", "http://token.einyun.com:8080/v1", "https://token.einyun.com.:443"} {
		allowed, err := policy.Allows(endpoint)
		require.NoError(t, err)
		assert.False(t, allowed, endpoint)
	}
	for _, endpoint := range []string{"https://token.einyun.com.attacker.test", "https://other.einyun.com"} {
		allowed, err := policy.Allows(endpoint)
		require.NoError(t, err)
		assert.True(t, allowed, endpoint)
	}
	for _, hostname := range []string{"https://token.einyun.com", "token.einyun.com:443", "*.einyun.com", "token.einyun.com/path", "bad_host"} {
		assert.Error(t, policy.AddGatewayHosts([]string{hostname}), hostname)
	}
}

func TestReporterRechecksQueuedGatewayHostAcrossPorts(t *testing.T) {
	cfg := ReporterConfig{Source: "zcode", Server: "https://report.example", Credential: "fixture", StatePath: t.TempDir() + "/state.json"}
	reporter, err := NewReporter(cfg)
	require.NoError(t, err)
	event := reporterTestEvent("gateway-host", "https://token.einyun.com:8443")
	require.NoError(t, reporter.Record(event))
	cfg.GatewayHosts = []string{"token.einyun.com"}
	restarted, err := NewReporter(cfg)
	require.NoError(t, err)
	stats, err := restarted.FlushContext(t.Context())
	require.NoError(t, err)
	assert.Zero(t, stats.Sent)
	assert.NoFileExists(t, reporter.queueDir+"/"+event.Event.EventID+".json")
}
