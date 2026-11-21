package identity

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// resolutions counts caller-identity resolutions by result (the Result*
	// constants). validator_unavailable is the fail-open counter: each one is
	// a call served unattributed because dashboard-be could not answer.
	resolutions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tas_mcp",
		Name:      "identity_resolutions_total",
		Help:      "Caller-identity resolutions at the MCP proxy, by result. validator_unavailable = served unattributed (fail-open).",
	}, []string{"result"})

	// cacheLookups counts validator-cache lookups (hit|miss).
	cacheLookups = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tas_mcp",
		Name:      "identity_cache_lookups_total",
		Help:      "Token-validation cache lookups at the MCP proxy, by result.",
	}, []string{"result"})
)
