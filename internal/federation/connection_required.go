package federation

import (
	"errors"
	"regexp"
	"slices"

	"go.uber.org/zap"
)

// ErrConnectionRequired — the server serves only tool calls that carry the
// caller tenant's stored database connection, and this call has none. It is
// never served on the server's own login.
var ErrConnectionRequired = errors.New(
	"connection required: this server needs a database connection stored for the caller's tenant")

// SetConnectionRequired names the servers that never serve a call on their own
// login (MCP_CONNECTION_REQUIRED_SERVERS). It does not depend on connection
// injection being installed: with injection off, every call to them is refused.
func (m *Manager) SetConnectionRequired(serverIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connRequired = slices.Clone(serverIDs)
}

func (m *Manager) connectionRequired(serverID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Contains(m.connRequired, serverID)
}

// withoutCallerConnection drops a caller-supplied connection before injection,
// so a connection found on the upstream request can only be an injected one.
func withoutCallerConnection(req *MCPRequest) *MCPRequest {
	if req.Method != methodToolsCall {
		return req
	}
	if c := withoutArguments(req, []string{defaultConnArgument}); c != nil {
		ConnectionInjections.WithLabelValues("stripped").Inc()
		return c
	}
	return req
}

// requireConnection checks what would go upstream, not what was resolved: a
// connection of another kind, or one injected under another argument name, is
// ignored by the bridge, which would then use the server's own login. Only
// tools/list is served without one.
//
// The upstream request is not serialized yet, so an injected connection is
// still the dbConnection that dsnValue built. A decoded caller request cannot
// hold that type, so never accept a map here.
func (m *Manager) requireConnection(serverID string, upstream *MCPRequest) error {
	if upstream.Method == methodToolsList {
		return nil
	}
	if upstream.Method == methodToolsCall {
		if args, ok := upstream.Params["arguments"].(map[string]interface{}); ok {
			if conn, ok := args[defaultConnArgument].(dbConnection); ok && conn.complete() && conn.plain() {
				return nil
			}
		}
	}
	ConnectionInjections.WithLabelValues("required").Inc()
	m.logger.Warn("federation: server requires a stored connection; call refused",
		zap.String("server_id", serverID), zap.String("method", upstream.Method))
	return ErrConnectionRequired
}

// complete reports whether every field the bridge reads is set. It fills an
// empty one from its own environment, which is the server's own login.
func (c *dbConnection) complete() bool {
	return c.Host != "" && c.Port > 0 && c.Username != "" && c.Password != "" && c.Database != ""
}

// plainConnField is what may go into the URL the bridge builds: it escapes the
// password and nothing else, so any other character could move the connection
// to a host the egress rule never saw (a database of "b?host=127.0.0.1").
var plainConnField = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// plain reports whether every unescaped field is safe in that URL.
func (c *dbConnection) plain() bool {
	return plainConnField.MatchString(c.Host) && plainConnField.MatchString(c.Username) &&
		plainConnField.MatchString(c.Database) && (c.SSLMode == "" || plainConnField.MatchString(c.SSLMode))
}
