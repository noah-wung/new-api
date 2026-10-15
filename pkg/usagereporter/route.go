package usagereporter

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// RoutePolicy applies gateway precedence to every client. A nil external list
// accepts known non-gateway origins; an empty list accepts no external origins.
type RoutePolicy struct {
	gateways           map[string]bool
	external           map[string]bool
	allowKnownExternal bool
	excludedRoutes     map[string]bool
}

var ErrUnapprovedRoute = errors.New("upstream is not an approved external route")

func NewRoutePolicy(server string, gateways, external []string, excludedRoutes ...string) (*RoutePolicy, error) {
	policy := &RoutePolicy{gateways: make(map[string]bool), external: make(map[string]bool), allowKnownExternal: external == nil, excludedRoutes: make(map[string]bool)}
	for _, id := range excludedRoutes {
		policy.excludedRoutes[id] = true
	}
	for _, value := range append([]string{server}, gateways...) {
		if strings.TrimSpace(value) == "" {
			continue
		}
		origin, err := NormalizeUpstreamURL(value)
		if err != nil {
			return nil, errors.New("invalid gateway URL in reporter configuration")
		}
		policy.gateways[origin] = true
	}
	for _, value := range external {
		origin, err := NormalizeUpstreamURL(value)
		if err != nil {
			return nil, errors.New("invalid external URL in reporter configuration")
		}
		policy.external[origin] = true
	}
	return policy, nil
}

// Allows returns false for a gateway, and an error for missing or unapproved
// route evidence. Model names never participate in route classification.
func (p *RoutePolicy) Allows(value string, routeIDs ...string) (bool, error) {
	for _, id := range routeIDs {
		if p.excludedRoutes[id] {
			return false, nil
		}
	}
	origin, err := NormalizeUpstreamURL(value)
	if err != nil {
		return false, ErrUnapprovedRoute
	}
	if p.gateways[origin] {
		return false, nil
	}
	if p.allowKnownExternal || p.external[origin] {
		return true, nil
	}
	return false, ErrUnapprovedRoute
}

func NormalizeUpstreamURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("route must be an HTTP(S) URL without credentials, query, or fragment")
	}
	origin, err := gatewayOrigin(value)
	if err != nil || strings.ContainsAny(origin, ",= \t\r\n") {
		return "", errors.New("invalid upstream origin")
	}
	return origin, nil
}

func gatewayOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" {
		return "", errors.New("expected an HTTP or HTTPS URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("expected an HTTP or HTTPS URL")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	port := parsed.Port()
	// Older Claude event IDs include this origin representation. Recognize a
	// legacy unbracketed IPv6 origin on subsequent normalization passes.
	if net.ParseIP(parsed.Host) != nil {
		host, port = strings.ToLower(parsed.Host), ""
	}
	if port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return "", errors.New("invalid upstream port")
		}
	}
	if port == "" || scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		return scheme + "://" + host, nil
	} else {
		host = net.JoinHostPort(host, port)
	}
	return scheme + "://" + host, nil
}
