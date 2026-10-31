package usagereporter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZCodeRoutesRequireEvidenceAndHonorGateway(t *testing.T) {
	policy, err := NewRoutePolicy("https://token.example", []string{"http://192.0.2.1:3000"}, nil)
	require.NoError(t, err)
	routes := []ZCodeProviderRoute{
		{ProviderID: "reused", Origin: "https://external.example/v1", From: 100, Until: 200},
		{ProviderID: "reused", Origin: "https://token.example/v1", From: 200, Until: 300},
		{ProviderID: "ip", Origin: "http://192.0.2.1:3000/v1", From: 100, Until: 300},
	}
	_, allowed, resolved, err := resolveZCodeRoute(policy, routes, nil, "reused", 150, 150)
	require.NoError(t, err)
	require.True(t, resolved && allowed)
	_, allowed, resolved, err = resolveZCodeRoute(policy, routes, nil, "reused", 200, 200)
	require.NoError(t, err)
	require.True(t, resolved && !allowed)
	_, allowed, resolved, err = resolveZCodeRoute(policy, routes, nil, "ip", 150, 150)
	require.NoError(t, err)
	require.True(t, resolved && !allowed)
	_, _, resolved, err = resolveZCodeRoute(policy, routes, nil, "reused", 199, 200)
	require.NoError(t, err)
	require.False(t, resolved, "a request spanning a route switch must not be classified as external")
	_, _, resolved, err = resolveZCodeRoute(policy, routes, nil, "reused", 99, 99)
	require.NoError(t, err)
	require.False(t, resolved)
	_, allowed, resolved, err = resolveZCodeRoute(policy, routes, []string{"reused"}, "reused", 150, 150)
	require.NoError(t, err)
	require.True(t, resolved && !allowed)
	routes = append(routes, ZCodeProviderRoute{ProviderID: "reused", Origin: "https://other.example", From: 100, Until: 200})
	_, _, resolved, err = resolveZCodeRoute(policy, routes, nil, "reused", 150, 150)
	require.NoError(t, err)
	require.False(t, resolved)
	partialOverlap := []ZCodeProviderRoute{
		{ProviderID: "p", Origin: "https://external.example", From: 100, Until: 300},
		{ProviderID: "p", Origin: "https://token.example", From: 150, Until: 200},
	}
	_, _, resolved, err = resolveZCodeRoute(policy, partialOverlap, nil, "p", 120, 250)
	require.NoError(t, err)
	require.False(t, resolved, "partially overlapping route evidence is ambiguous")
}
