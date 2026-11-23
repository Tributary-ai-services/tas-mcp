package federation

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// DSN egress (MCP-19).
//
// A stored database connection (C.4) names a host and a port the postgres-mcp
// bridge will dial. Q13's namespace rule alone cannot let a tenant reach the
// shared PostgreSQL in tas-shared without also exposing Redis, Keycloak, Kafka
// and MinIO in the same namespace, so a DSN host is checked against exact
// host:port pairs instead:
//
//  1. Always refused, even when listed: localhost names, loopback, link-local
//     (which includes the cloud metadata address 169.254.169.254), the AWS
//     IPv6 metadata address, unspecified and multicast addresses, and a host
//     that looks numeric but is not a plain dotted-quad or IPv6 literal
//     ("2130706433", "0x7f.0.0.1", "127.1"): resolvers read those as IPs.
//  2. Allowed: a pair on MCP_DSN_EGRESS_ALLOW, the platform list, for every
//     tenant (e.g. postgres-shared.tas-shared.svc.cluster.local:5432).
//  3. With the tenant registry off, only a public DNS name passes
//     (basicHostCheck, as before).
//  4. Allowed: a service in an allowed namespace (Q13, unchanged). Any other
//     cluster name, and a bare name, is refused.
//  5. An IP literal, private or public, passes only when the tenant's
//     MCP_TENANT_EGRESS_ALLOWLIST lists it.
//  6. Allowed: a name on the tenant's allowlist. An entry may carry a port
//     ("db.partner.example:6543"), and then matches that port only; an entry
//     without one matches any port, as it did before MCP-19.
//
// The rule is a string rule: the proxy and aiqg-dashboard-be never dial a DSN,
// so they cannot be rebound. The bridge, which dials, applies the same rule
// and then checks every resolved address and connects to the address it
// checked (tas-mcp-servers bridge/src/egress.js). aiqg-dashboard-be has an
// identical copy (internal/mcpconfig/egress.go); testdata/dsn_egress_vectors.json
// is the same file in all three repos and each one's tests run it.

// HostPort is one MCP_DSN_EGRESS_ALLOW entry. Host is canonical (canonicalHost).
type HostPort struct {
	Host string
	Port int
}

func (h HostPort) String() string { return net.JoinHostPort(h.Host, strconv.Itoa(h.Port)) }

// ParseDSNEgressAllow parses MCP_DSN_EGRESS_ALLOW: comma-separated host:port
// pairs. Every entry needs an explicit port. One bad entry, or an entry the
// rule always refuses, fails the whole list: the caller then allows none
// (fail closed) and logs the error.
func ParseDSNEgressAllow(raw string) ([]HostPort, error) {
	var out []HostPort
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		h, p, err := net.SplitHostPort(item)
		if err != nil {
			return nil, fmt.Errorf("MCP_DSN_EGRESS_ALLOW: %q is not host:port", item)
		}
		port, err := strconv.Atoi(p)
		if err != nil || !validPort(port) {
			return nil, fmt.Errorf("MCP_DSN_EGRESS_ALLOW: %q has no valid port", item)
		}
		host := canonicalHost(h)
		if host == "" || strings.HasPrefix(host, "*") {
			return nil, fmt.Errorf("MCP_DSN_EGRESS_ALLOW: %q must name one host (no wildcards)", item)
		}
		if err := alwaysDenied(host); err != nil {
			return nil, fmt.Errorf("MCP_DSN_EGRESS_ALLOW: %q: %w", item, err)
		}
		out = append(out, HostPort{Host: host, Port: port})
	}
	return out, nil
}

func validPort(p int) bool { return p > 0 && p <= 65535 }

// canonicalHost lowercases a host, drops IPv6 brackets and a trailing dot,
// writes an IP in its canonical form (an IPv4-mapped IPv6 address as IPv4),
// and writes a service name <svc>.<ns>.svc in its .svc.cluster.local form, so
// the two spellings of one service compare equal.
func canonicalHost(raw string) string {
	h := strings.ToLower(strings.TrimSuffix(strings.Trim(strings.TrimSpace(raw), "[]"), "."))
	if a, err := netip.ParseAddr(h); err == nil {
		return a.Unmap().WithZone("").String()
	}
	if strings.HasSuffix(h, ".svc") {
		h += ".cluster.local"
	}
	return h
}

const hostLocalhost = "localhost"

// errAlwaysDenied marks a host no list can allow.
var errAlwaysDenied = errors.New("never allowed")

// alwaysDenied refuses a canonical host that no allowlist may open.
func alwaysDenied(host string) error {
	if host == hostLocalhost || strings.HasSuffix(host, "."+hostLocalhost) {
		return fmt.Errorf("%w: %q is localhost", errAlwaysDenied, host)
	}
	if a, err := netip.ParseAddr(host); err == nil {
		if reason := deniedAddr(a); reason != "" {
			return fmt.Errorf("%w: %s is %s", errAlwaysDenied, host, reason)
		}
		return nil
	}
	if ambiguousNumeric(host) {
		return fmt.Errorf("%w: %q looks like an IP address in a non-standard form", errAlwaysDenied, host)
	}
	return nil
}

// awsMetadataV6 is the EC2 instance metadata service's IPv6 address.
var awsMetadataV6 = netip.MustParseAddr("fd00:ec2::254")

// deniedAddr names why an address may never be dialed, or "" when it may be
// (subject to the lists).
func deniedAddr(a netip.Addr) string {
	a = a.Unmap()
	switch {
	case a.IsLoopback():
		return "loopback"
	case a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast():
		return "link-local (includes the metadata address)"
	case a == awsMetadataV6:
		return "the metadata address"
	case a.IsUnspecified():
		return "unspecified"
	case a.IsMulticast(), a.IsInterfaceLocalMulticast():
		return "multicast"
	}
	return ""
}

// ambiguousNumeric reports a host whose last label is a number (no TLD is) or
// starts with 0x: getaddrinfo reads "127.1", "0177.0.0.1" and "2130706433" as
// IPv4, so such a name must never be treated as a DNS name.
func ambiguousNumeric(host string) bool {
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	if last == "" {
		return false
	}
	if strings.HasPrefix(last, "0x") {
		return true
	}
	for _, r := range last {
		if r < '0' || r > '9' {
			return strings.HasPrefix(labels[0], "0x")
		}
	}
	return true
}

// DSNRule is the DSN egress rule's configuration.
type DSNRule struct {
	// Platform is MCP_DSN_EGRESS_ALLOW, allowed for every tenant.
	Platform []HostPort
	// Policy is the tenant registry (its per-tenant allowlist and cluster
	// namespaces); nil = registry off.
	Policy *TenantPolicy
}

// Check applies the DSN egress rule to host:port for tenant.
func (r DSNRule) Check(tenant, rawHost string, port int) error {
	host := canonicalHost(rawHost)
	if host == "" {
		return fmt.Errorf("%w: no host", ErrEgressDenied)
	}
	if !validPort(port) {
		return fmt.Errorf("%w: port %d is not a valid port", ErrEgressDenied, port)
	}
	if err := alwaysDenied(host); err != nil {
		return fmt.Errorf("%w: %w", ErrEgressDenied, err)
	}
	for _, hp := range r.Platform {
		if hp.Host == host && hp.Port == port {
			return nil
		}
	}
	if r.Policy == nil {
		return basicHostCheck(host)
	}
	p := r.Policy
	if ns, ok := clusterNamespace(host); ok {
		for _, allowed := range p.clusterNamespaces() {
			if ns == allowed {
				return nil
			}
		}
		return fmt.Errorf("%w: %s:%d is not on MCP_DSN_EGRESS_ALLOW, and in-cluster databases must otherwise be in namespace %s, not %q",
			ErrEgressDenied, host, port, strings.Join(p.clusterNamespaces(), ", "), ns)
	}
	if isClusterInternal(host) {
		return fmt.Errorf("%w: host %q is cluster-internal", ErrEgressDenied, host)
	}
	if !strings.Contains(host, ".") && !isIPLiteral(host) {
		return fmt.Errorf("%w: host %q is a bare name; use <service>.<namespace>.svc", ErrEgressDenied, host)
	}
	if dsnHostAllowed(host, port, p.Egress[tenant]) {
		return nil
	}
	return fmt.Errorf("%w: %s:%d is not on MCP_DSN_EGRESS_ALLOW or this tenant's egress allowlist (MCP_TENANT_EGRESS_ALLOWLIST)",
		ErrEgressDenied, host, port)
}

func isIPLiteral(host string) bool {
	_, err := netip.ParseAddr(host)
	return err == nil
}

// dsnHostAllowed matches a canonical host and port against a tenant's
// allowlist entries ("host", "host:port", "*.domain", "*.domain:port", an IP,
// "[v6]:port"). An IP literal matches only an IP entry, never a wildcard.
func dsnHostAllowed(host string, port int, allow []string) bool {
	ip := isIPLiteral(host)
	for _, raw := range allow {
		eh, ep, ok := splitAllowEntry(raw)
		if !ok || (ep != 0 && ep != port) {
			continue
		}
		switch {
		case strings.HasPrefix(eh, "*."):
			// eh[1:] keeps the dot, so the apex itself never matches.
			if !ip && strings.HasSuffix(host, eh[1:]) {
				return true
			}
		case eh == host:
			return true
		}
	}
	return false
}

// splitAllowEntry reads one tenant allowlist entry into a canonical host and a
// port (0 = any). ok is false for an empty or malformed entry.
func splitAllowEntry(raw string) (host string, port int, ok bool) {
	e := strings.TrimSpace(raw)
	if e == "" {
		return "", 0, false
	}
	if h, p, err := net.SplitHostPort(e); err == nil {
		n, err := strconv.Atoi(p)
		if err != nil || !validPort(n) {
			return "", 0, false
		}
		return canonicalHost(h), n, true
	}
	return canonicalHost(e), 0, true
}
