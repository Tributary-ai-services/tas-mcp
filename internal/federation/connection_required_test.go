package federation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func requiredEnv(t *testing.T) *connEnv {
	t.Helper()
	e := newConnEnv(t)
	e.m.SetConnectionRequired([]string{srvPlat})
	return e
}

func callerConn() map[string]interface{} {
	return map[string]interface{}{"sql": selectOne, "_connection": map[string]interface{}{"host": "attacker.example"}}
}

func dsnConn(params map[string]string) *Connection {
	return &Connection{ID: "b", Kind: ConnKindDatabaseDSN, Version: "1", Params: params,
		Secret: "postgres://b:pw-bbbbbbbb@pg.vendor.example/b"}
}

func TestRequired_RefusesWithoutUsableConnection(t *testing.T) {
	for _, c := range []struct {
		name, token string
		conn        *Connection
		injectorOff bool
	}{
		{name: "no token"},
		{name: "unknown token", token: "not-a-known-token"},
		{name: "tenant without a connection", token: tokenA},
		{name: "header connection", token: tokenB, conn: headerConn("b", "hdr-secret-bbbb")},
		{name: "another argument name", token: tokenB, conn: dsnConn(map[string]string{"argument": "dsn"})},
		{name: "string format", token: tokenB, conn: dsnConn(map[string]string{"format": "string"})},
		{name: "injection off", token: tokenA, injectorOff: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := requiredEnv(t)
			if c.conn != nil {
				e.res.set(tenantB, srvPlat, c.conn)
			}
			if c.injectorOff {
				e.m.SetConnectionInjector(nil)
			}
			for _, args := range []map[string]interface{}{nil, {"sql": selectOne}, callerConn()} {
				if rec := e.call(t, srvPlat, callBody("query", args), as(c.token)); rec.Code != http.StatusForbidden {
					t.Fatalf("args %v = %d %s, want 403", args, rec.Code, rec.Body)
				}
			}
			if n := e.up.count(); n != 0 {
				t.Fatalf("upstream reached %d times", n)
			}
		})
	}
}

func TestRequired_ServesStoredConnection(t *testing.T) {
	e := requiredEnv(t)
	e.res.set(tenantB, srvPlat, dsnConn(nil))
	if rec := e.call(t, srvPlat, callBody("query", callerConn()), as(tokenB)); rec.Code != http.StatusOK {
		t.Fatalf("stored connection = %d %s", rec.Code, rec.Body)
	}
	conn, _ := e.up.lastSeen(t).Args["_connection"].(map[string]interface{})
	if conn["host"] != "pg.vendor.example" {
		t.Fatalf("upstream connection %v", conn)
	}
}

// The gate runs before serialization. The caller's JSON object is refused even
// if it has every field and stripping missed it; only the injector's own
// complete value passes.
func TestRequired_AcceptsOnlyInjectedCompleteConnection(t *testing.T) {
	e := requiredEnv(t)
	full := dbConnection{Host: "h.example", Port: portPostgres, Username: "u", Password: "p", Database: "d"}
	with := func(edit func(*dbConnection)) dbConnection {
		c := full
		edit(&c)
		return c
	}
	for _, c := range []struct {
		name  string
		value interface{}
		want  error
	}{
		{name: "injected", value: full},
		{name: "decoded from the wire", want: ErrConnectionRequired, value: map[string]interface{}{
			"host": "h.example", "port": float64(portPostgres), "username": "u", "password": "p", "database": "d"}},
		{name: "zero", value: dbConnection{}, want: ErrConnectionRequired},
		{name: "no host", value: with(func(c *dbConnection) { c.Host = "" }), want: ErrConnectionRequired},
		{name: "no port", value: with(func(c *dbConnection) { c.Port = 0 }), want: ErrConnectionRequired},
		{name: "negative port", value: with(func(c *dbConnection) { c.Port = -1 }), want: ErrConnectionRequired},
		{name: "no username", value: with(func(c *dbConnection) { c.Username = "" }), want: ErrConnectionRequired},
		{name: "no password", value: with(func(c *dbConnection) { c.Password = "" }), want: ErrConnectionRequired},
		{name: "query in database", value: with(func(c *dbConnection) { c.Database = "b?host=127.0.0.1&port=6000" }), want: ErrConnectionRequired},
		{name: "query in sslmode", value: with(func(c *dbConnection) { c.SSLMode = "disable&host=127.0.0.1" }), want: ErrConnectionRequired},
		{name: "host in username", value: with(func(c *dbConnection) { c.Username = "u@127.0.0.1/x#" }), want: ErrConnectionRequired},
		{name: "space in username", value: with(func(c *dbConnection) { c.Username = " " }), want: ErrConnectionRequired},
		{name: "IPv6 host", value: with(func(c *dbConnection) { c.Host = "::1" }), want: ErrConnectionRequired},
		{name: "plain sslmode", value: with(func(c *dbConnection) { c.SSLMode = "verify-full" })},
		{name: "no database", value: with(func(c *dbConnection) { c.Database = "" }), want: ErrConnectionRequired},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := &MCPRequest{Method: methodToolsCall, Params: map[string]interface{}{
				"arguments": map[string]interface{}{defaultConnArgument: c.value}}}
			if err := e.m.requireConnection(srvPlat, req); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

func TestRequired_Methods(t *testing.T) {
	e := requiredEnv(t)
	for _, method := range []string{"resources/read", "resources/list", "TOOLS/CALL", "tools/call ", ""} {
		body := `{"id":"1","method":"` + method + `"}`
		if rec := e.call(t, srvPlat, body, as(tokenA)); rec.Code != http.StatusForbidden {
			t.Fatalf("%q = %d, want 403", method, rec.Code)
		}
	}
	if rec := e.call(t, srvPlat, `{"id":"1","method":"tools/list"}`, nil); rec.Code == http.StatusForbidden {
		t.Fatal("tools/list must stay open")
	}
}

func TestRequired_OtherServersUnchanged(t *testing.T) {
	e := newConnEnv(t)
	e.m.SetConnectionRequired([]string{"some-other-server"})
	if rec := e.call(t, srvPlat, callBody("query", nil), nil); rec.Code != http.StatusOK {
		t.Fatalf("server not on the list = %d", rec.Code)
	}
}

// Broadcast has no injection step, so it must not reach a required server.
func TestRequired_BroadcastSkips(t *testing.T) {
	for _, required := range []bool{false, true} {
		e := newConnEnv(t)
		ctx := context.Background()
		s, err := e.m.registry.Get(ctx, srvPlat)
		if err != nil {
			t.Fatal(err)
		}
		s.Status = StatusHealthy
		if err := e.m.registry.Put(ctx, s); err != nil {
			t.Fatal(err)
		}
		if _, err := e.m.ensureService(ctx, srvPlat); err != nil {
			t.Fatal(err)
		}
		if required {
			e.m.SetConnectionRequired([]string{srvPlat})
		}
		_, _ = e.m.BroadcastRequest(ctx, &MCPRequest{ID: "b", Method: methodToolsCall,
			Params: map[string]interface{}{"name": "query", "arguments": callerConn()}})
		if reached := e.up.count() > 0; reached == required {
			t.Fatalf("required=%v: upstream reached=%v", required, reached)
		}
	}
}

// The HTTP client refuses unknown methods by itself, so only a transport that
// forwards any method (Streamable HTTP, as DBHub uses) proves the broadcast
// rule for methods other than tools/call.
func TestRequired_BroadcastOnlyToolsList(t *testing.T) {
	for _, method := range []string{methodToolsList, methodToolsCall, "resources/read", "TOOLS/LIST", "tools/list ", ""} {
		e := newConnEnv(t)
		reached := false
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			_, _ = w.Write([]byte(`{"result":{}}`))
		}))
		t.Cleanup(up.Close)
		ctx := context.Background()
		srv := &MCPServer{ID: "dbhub", Endpoint: up.URL, Protocol: ProtocolSSE, Auth: AuthConfig{Type: AuthNone}, Status: StatusHealthy}
		if err := e.m.registry.Put(ctx, srv); err != nil {
			t.Fatal(err)
		}
		if _, err := e.m.ensureService(ctx, srv.ID); err != nil {
			t.Fatal(err)
		}
		e.m.SetConnectionRequired([]string{srv.ID})
		if _, err := e.m.BroadcastRequest(ctx, &MCPRequest{ID: "b", Method: method}); err != nil {
			t.Fatal(err)
		}
		if want := method == methodToolsList; reached != want {
			t.Fatalf("%q: upstream reached=%v, want %v", method, reached, want)
		}
	}
}
