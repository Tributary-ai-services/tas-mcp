package federation

import (
	"context"
	"errors"
	"sync"
)

// ErrServerNotFound is returned by Registry.Get and Registry.Delete when no
// server is registered under the given id.
var ErrServerNotFound = errors.New("server not found")

// RegistryEventType is the kind of change a Watch reports.
type RegistryEventType string

const (
	// RegistryPut means a server definition was added or updated.
	RegistryPut RegistryEventType = "put"
	// RegistryDelete means a server definition was removed.
	RegistryDelete RegistryEventType = "delete"
)

// RegistryEvent is a change notification from Watch. It carries only the type
// and id (small pub/sub payload); consumers Get the definition if they need it.
type RegistryEvent struct {
	Type RegistryEventType
	ID   string
}

// watchChannelBuffer is the buffer on a Watch channel — enough to absorb a burst
// of changes without stalling a Put/Delete, small enough to bound memory.
const watchChannelBuffer = 16

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
// Implementations: memoryRegistry (default, single-pod) and redisRegistry
// (shared, with a pub/sub Watch). The Manager builds live MCPService connections
// lazily from these definitions (Manager.ensureService), so any replica can
// serve any invoke, and drops them on a Watch delete.
type Registry interface {
	// Put upserts a server definition.
	Put(ctx context.Context, server *MCPServer) error

	// Get returns the definition for id, or ErrServerNotFound if absent.
	Get(ctx context.Context, id string) (*MCPServer, error)

	// Delete removes the definition for id, or returns ErrServerNotFound.
	Delete(ctx context.Context, id string) error

	// List returns all server definitions.
	List(ctx context.Context) ([]*MCPServer, error)

	// Watch returns a channel of change events so a replica can converge its
	// local state (e.g. drop the live MCPService for a deleted server). The
	// channel is closed when ctx is canceled.
	Watch(ctx context.Context) (<-chan RegistryEvent, error)
}

// memoryRegistry is the default, in-process Registry — the behavior the Manager
// had before the abstraction. It stores live *MCPServer pointers, so the
// Manager's in-place status updates persist through Get (a serializing impl like
// Redis will instead round-trip copies, and the Manager's status writes become
// read-modify-Put in Slice 2).
type memoryRegistry struct {
	mu      sync.RWMutex
	servers map[string]*MCPServer
	subs    map[chan RegistryEvent]struct{}
}

// NewMemoryRegistry returns an empty in-process Registry.
func NewMemoryRegistry() Registry {
	return &memoryRegistry{
		servers: make(map[string]*MCPServer),
		subs:    make(map[chan RegistryEvent]struct{}),
	}
}

func (r *memoryRegistry) Put(_ context.Context, server *MCPServer) error {
	if server == nil {
		return errors.New("server cannot be nil")
	}
	if server.ID == "" {
		return errors.New("server ID cannot be empty")
	}
	r.mu.Lock()
	r.servers[server.ID] = server
	r.notify(RegistryEvent{Type: RegistryPut, ID: server.ID})
	r.mu.Unlock()
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
	r.notify(RegistryEvent{Type: RegistryDelete, ID: id})
	return nil
}

// Watch registers a subscriber and returns its event channel, closed when ctx
// is canceled.
func (r *memoryRegistry) Watch(ctx context.Context) (<-chan RegistryEvent, error) {
	ch := make(chan RegistryEvent, watchChannelBuffer)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()

	go func() {
		<-ctx.Done()
		r.mu.Lock()
		delete(r.subs, ch)
		close(ch)
		r.mu.Unlock()
	}()
	return ch, nil
}

// notify fans an event out to subscribers. Callers hold r.mu, which serializes
// with Watch's subscribe/close, so sends never race a close; sends are
// non-blocking so a slow consumer can't stall a Put/Delete.
func (r *memoryRegistry) notify(evt RegistryEvent) {
	for ch := range r.subs {
		select {
		case ch <- evt:
		default:
		}
	}
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
