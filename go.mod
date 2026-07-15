module github.com/tributary-ai-services/tas-mcp

go 1.24.0

toolchain go1.24.4

require (
	github.com/Tributary-ai-services/Gatekeeper v0.0.0-00010101000000-000000000000
	github.com/gorilla/mux v1.8.1
	go.uber.org/zap v1.26.0
	golang.org/x/oauth2 v0.34.0
	google.golang.org/grpc v1.79.0
	google.golang.org/protobuf v1.36.10
)

// Gatekeeper is developed in-tree alongside tas-mcp; resolve it (and its
// transitive in-tree deps) to the local monorepo checkout, mirroring
// tas-llm-router. Only pkg/extract is imported, which is pure-Go (no Hyperscan/
// CGO), so this does not change tas-mcp's build requirements.
replace github.com/Tributary-ai-services/Gatekeeper => ../Gatekeeper

require (
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)
