package federation

import (
	"context"
	"errors"
	"sync"
)

// ErrServerNotFound is returned by Registry.Get and Registry.Delete when no
// server is registered under the given id.
var ErrServerNotFound = errors.New("server not found")

// Registry is the source of truth for federated server DEFINITIONS — the
// serializable MCPServer metadata (id, endpoint, protocol, auth, capabilities,
// status). It is deliberately separate from the live, per-pod MCPService
// connections the Manager builds from those definitions.
//
// Why: the gateway runs multiple replicas, but the definition store had been a
// per-pod in-memory map, so registrations on one replica were invisible to the
// others (inconsistent list/invoke, orphaned entries). A shared implementation
// (e.g. Redis) makes every replica agree on the federated set. See issue #19.
//
// This is Slice 1: the interface + the default in-process memoryRegistry, with
// no behavior change. Slice 2 adds a redisRegistry, a Watch for cross-replica
// propagation, and lazy MCPService self-heal in Manager.InvokeServer.
type Registry interface {
	// Put upserts a server definition.
	Put(ctx context.Context, server *MCPServer) error

	// Get returns the definition for id, or ErrServerNotFound if absent.
	Get(ctx context.Context, id string) (*MCPServer, error)

	// Delete removes the definition for id, or returns ErrServerNotFound.
	Delete(ctx context.Context, id string) error

	// List returns all server definitions.
	List(ctx context.Context) ([]*MCPServer, error)
}

// memoryRegistry is the default, in-process Registry — the behavior the Manager
// had before the abstraction. It stores live *MCPServer pointers, so the
// Manager's in-place status updates persist through Get (a serializing impl like
// Redis will instead round-trip copies, and the Manager's status writes become
// read-modify-Put in Slice 2).
type memoryRegistry struct {
	mu      sync.RWMutex
	servers map[string]*MCPServer
}

// NewMemoryRegistry returns an empty in-process Registry.
func NewMemoryRegistry() Registry {
	return &memoryRegistry{servers: make(map[string]*MCPServer)}
}

func (r *memoryRegistry) Put(_ context.Context, server *MCPServer) error {
	if server == nil {
		return errors.New("server cannot be nil")
	}
	if server.ID == "" {
		return errors.New("server ID cannot be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[server.ID] = server
	return nil
}

func (r *memoryRegistry) Get(_ context.Context, id string) (*MCPServer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	server, ok := r.servers[id]
	if !ok {
		return nil, ErrServerNotFound
	}
	return server, nil
}

func (r *memoryRegistry) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.servers[id]; !ok {
		return ErrServerNotFound
	}
	delete(r.servers, id)
	return nil
}

func (r *memoryRegistry) List(_ context.Context) ([]*MCPServer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	servers := make([]*MCPServer, 0, len(r.servers))
	for _, s := range r.servers {
		servers = append(servers, s)
	}
	return servers, nil
}
