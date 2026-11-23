package federation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

// dsnEgressVectorsSHA256 pins testdata/dsn_egress_vectors.json. The same file
// is in aiqg-dashboard-be (internal/mcpconfig/testdata) and tas-mcp-servers
// (bridge/test), each pinned to this hash, so the proxy, AIQG and the bridge
// are tested against one set of answers. Change all three files and all three
// constants together.
const dsnEgressVectorsSHA256 = "2a2c2469c79b6d36a82fa85836921e1206f7788066bb82709ff9f08c15f4f778"

type dsnVectors struct {
	Config struct {
		Tenants        map[string]string   `json:"tenants"`
		DSNEgressAllow string              `json:"dsn_egress_allow"`
		TenantAllow    map[string][]string `json:"tenant_egress_allowlist"`
		Namespaces     []string            `json:"cluster_namespaces"`
	} `json:"config"`
	Cases []struct {
		Name     string `json:"name"`
		Tenant   string `json:"tenant"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Registry *bool  `json:"registry"`
		Allow    bool   `json:"allow"`
	} `json:"cases"`
}

func loadDSNVectors(t *testing.T) dsnVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/dsn_egress_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != dsnEgressVectorsSHA256 {
		t.Fatalf("dsn_egress_vectors.json changed (sha256 %s): copy it to aiqg-dashboard-be and tas-mcp-servers and update the constant in all three", got)
	}
	var v dsnVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) < 50 {
		t.Fatalf("only %d vectors", len(v.Cases))
	}
	return v
}

// TestDSNEgress_Vectors runs the shared vectors through the proxy's rule.
func TestDSNEgress_Vectors(t *testing.T) {
	v := loadDSNVectors(t)
	platform, err := ParseDSNEgressAllow(v.Config.DSNEgressAllow)
	if err != nil || len(platform) != 1 {
		t.Fatalf("platform list: %v %v", platform, err)
	}
	on := DSNRule{Platform: platform, Policy: &TenantPolicy{Egress: v.Config.TenantAllow, ClusterNamespaces: v.Config.Namespaces}}
	off := DSNRule{Platform: platform}
	for _, c := range v.Cases {
		rule := on
		if c.Registry != nil && !*c.Registry {
			rule = off
		}
		tenant := v.Config.Tenants[c.Tenant]
		if tenant == "" {
			t.Fatalf("%s: unknown tenant %q", c.Name, c.Tenant)
		}
		err := rule.Check(tenant, c.Host, c.Port)
		if (err == nil) != c.Allow {
			t.Errorf("%s: %s:%d allowed=%v, want %v (%v)", c.Name, c.Host, c.Port, err == nil, c.Allow, err)
		}
		if err != nil && !errors.Is(err, ErrEgressDenied) {
			t.Errorf("%s: refusal does not wrap ErrEgressDenied: %v", c.Name, err)
		}
	}
}

func TestParseDSNEgressAllow(t *testing.T) {
	got, err := ParseDSNEgressAllow(" postgres-shared.tas-shared.svc:5432 , [2001:db8::5]:5432,,")
	if err != nil || len(got) != 2 || got[0].String() != "postgres-shared.tas-shared.svc.cluster.local:5432" ||
		got[1].String() != "[2001:db8::5]:5432" {
		t.Fatalf("got %v %v", got, err)
	}
	if got, err := ParseDSNEgressAllow(""); err != nil || got != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
	for _, bad := range []string{
		"postgres-shared.tas-shared.svc.cluster.local", // no port
		"postgres-shared.tas-shared.svc.cluster.local:0",
		"postgres-shared.tas-shared.svc.cluster.local:http",
		"*.tas-shared.svc.cluster.local:5432", // no wildcards
		"127.0.0.1:5432", "localhost:5432", "169.254.169.254:80", "[::1]:5432", "127.1:5432",
		"ok.example:5432,bad", // one bad entry fails the list
	} {
		if got, err := ParseDSNEgressAllow(bad); err == nil {
			t.Errorf("%q accepted: %v", bad, got)
		}
	}
}

// TestDSNEgress_Injection: end to end through the injector, the platform list
// opens the shared PostgreSQL and nothing else in tas-shared, and a refused
// host never reaches the bridge.
func TestDSNEgress_Injection(t *testing.T) {
	platform, err := ParseDSNEgressAllow("postgres-shared.tas-shared.svc.cluster.local:5432")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, dsn string
		ok        bool
	}{
		{"shared postgres", "postgres://u:pw-12345678@postgres-shared.tas-shared.svc.cluster.local:5432/org_db", true},
		{"shared postgres, default port", "postgresql://u:pw-12345678@postgres-shared.tas-shared.svc/org_db", true},
		{"shared postgres, other port", "postgres://u:pw-12345678@postgres-shared.tas-shared.svc.cluster.local:5433/org_db", false},
		{"redis in tas-shared", "postgres://u:pw-12345678@redis-shared.tas-shared.svc.cluster.local:6379/0", false},
		{"redis on the postgres port", "postgres://u:pw-12345678@redis-shared.tas-shared.svc.cluster.local:5432/0", false},
		{"mysql scheme default port", "mysql://u:pw-12345678@postgres-shared.tas-shared.svc.cluster.local/db", false},
		{"metadata", "postgres://u:pw-12345678@169.254.169.254:5432/db", false},
		{"loopback", "postgres://u:pw-12345678@[::1]:5432/db", false},
	} {
		for _, format := range []string{connFormatObject, connFormatString} {
			e := newConnEnv(t)
			e.m.connections.DSNAllow = platform
			e.res.set(tenantA, srvPlat, &Connection{ID: "d", Kind: ConnKindDatabaseDSN, Version: "1",
				Secret: c.dsn, Params: map[string]string{"format": format}})
			rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA))
			if c.ok != (rec.Code == http.StatusOK) || (!c.ok && e.up.count() != 0) {
				t.Errorf("%s (%s): %d, upstream calls %d", c.name, format, rec.Code, e.up.count())
			}
			if strings.Contains(rec.Body.String(), "pw-12345678") {
				t.Errorf("%s (%s): response quotes the password", c.name, format)
			}
		}
	}
	// Without the platform list the shared PostgreSQL stays refused (the
	// default is deny).
	e := newConnEnv(t)
	e.res.set(tenantA, srvPlat, &Connection{ID: "d", Kind: ConnKindDatabaseDSN, Version: "1",
		Secret: "postgres://u:pw-12345678@postgres-shared.tas-shared.svc.cluster.local:5432/org_db"})
	if rec := e.call(t, srvPlat, callBody("query", nil), as(tokenA)); rec.Code == http.StatusOK || e.up.count() != 0 {
		t.Fatalf("no MCP_DSN_EGRESS_ALLOW: %d, upstream calls %d", rec.Code, e.up.count())
	}
}

// The platform list is for DSNs only: it never opens a tenant server endpoint.
func TestDSNEgress_NotForEndpoints(t *testing.T) {
	p := &TenantPolicy{}
	if err := p.checkEgress(tenantA, "http://postgres-shared.tas-shared.svc.cluster.local:5432/mcp"); err == nil {
		t.Fatal("tenant server endpoint in tas-shared accepted")
	}
}
