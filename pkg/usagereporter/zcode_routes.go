package usagereporter

import (
	"errors"
	"strings"
)

// ZCodeProviderRoute records an explicitly verified provider route for a
// bounded period. Timestamps are Unix seconds, with an exclusive end.
type ZCodeProviderRoute struct {
	ProviderID string `json:"provider_id"`
	Origin     string `json:"origin"`
	From       int64  `json:"from"`
	Until      int64  `json:"until"`
}

// resolveZCodeRoute never infers a historic route from a current configuration.
// An explicit provider exclusion is decisive, as is a gateway origin. Ambiguous
// or incomplete evidence stays unresolved rather than being reported.
func resolveZCodeRoute(policy *RoutePolicy, routes []ZCodeProviderRoute, excluded []string, provider string, startedAt, occurredAt int64) (origin string, allowed, resolved bool, err error) {
	provider = strings.TrimSpace(provider)
	for _, id := range excluded {
		if provider != "" && strings.TrimSpace(id) == provider {
			return "", false, true, nil
		}
	}
	if policy == nil || provider == "" || startedAt <= 0 || occurredAt < startedAt {
		return "", false, false, nil
	}
	matches := 0
	covered := false
	for _, route := range routes {
		if strings.TrimSpace(route.ProviderID) != provider {
			continue
		}
		if route.From <= 0 || route.Until <= route.From {
			return "", false, false, errors.New("invalid ZCode route evidence")
		}
		if route.From > occurredAt || route.Until <= startedAt {
			continue
		}
		normalized, normalizeErr := NormalizeUpstreamURL(route.Origin)
		if normalizeErr != nil {
			return "", false, false, errors.New("invalid ZCode route evidence")
		}
		matches++
		if matches > 1 {
			return "", false, false, nil
		}
		origin = normalized
		covered = route.From <= startedAt && occurredAt < route.Until
	}
	if matches == 0 || !covered {
		return "", false, false, nil
	}
	allowed, err = policy.Allows(origin, sha256Hex(provider))
	if errors.Is(err, ErrUnapprovedRoute) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	return origin, allowed, true, nil
}
