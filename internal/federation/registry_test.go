package federation

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryRegistry_PutGet(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()

	s := &MCPServer{ID: "git", Name: "Git", Endpoint: "http://git:3000"}
	if err := r.Put(ctx, s); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := r.Get(ctx, "git")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Git" || got.Endpoint != "http://git:3000" {
		t.Errorf("unexpected server: %+v", got)
	}
}

func TestMemoryRegistry_GetMissing(t *testing.T) {
	r := NewMemoryRegistry()
	_, err := r.Get(context.Background(), "nope")
	if !errors.Is(err, ErrServerNotFound) {
		t.Errorf("want ErrServerNotFound, got %v", err)
	}
}

func TestMemoryRegistry_PutValidation(t *testing.T) {
	r := NewMemoryRegistry()
	if err := r.Put(context.Background(), nil); err == nil {
		t.Error("Put(nil) should error")
	}
	if err := r.Put(context.Background(), &MCPServer{ID: ""}); err == nil {
		t.Error("Put with empty ID should error")
	}
}

func TestMemoryRegistry_Delete(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()

	if err := r.Delete(ctx, "ghost"); !errors.Is(err, ErrServerNotFound) {
		t.Errorf("Delete missing: want ErrServerNotFound, got %v", err)
	}

	_ = r.Put(ctx, &MCPServer{ID: "db"})
	if err := r.Delete(ctx, "db"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(ctx, "db"); !errors.Is(err, ErrServerNotFound) {
		t.Error("server should be gone after Delete")
	}
}

func TestMemoryRegistry_List(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()

	if all, _ := r.List(ctx); len(all) != 0 {
		t.Errorf("empty registry should list 0, got %d", len(all))
	}
	_ = r.Put(ctx, &MCPServer{ID: "a"})
	_ = r.Put(ctx, &MCPServer{ID: "b"})
	all, err := r.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("want 2 servers, got %d", len(all))
	}
}

// Put upsert: a second Put for the same id replaces the definition.
func TestMemoryRegistry_PutUpsert(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()
	_ = r.Put(ctx, &MCPServer{ID: "x", Endpoint: "http://old"})
	_ = r.Put(ctx, &MCPServer{ID: "x", Endpoint: "http://new"})

	got, _ := r.Get(ctx, "x")
	if got.Endpoint != "http://new" {
		t.Errorf("upsert not applied: %q", got.Endpoint)
	}
	if all, _ := r.List(ctx); len(all) != 1 {
		t.Errorf("upsert should not duplicate: got %d", len(all))
	}
}

func TestMemoryRegistry_Watch(t *testing.T) {
	r := NewMemoryRegistry()
	ctx, cancel := context.WithCancel(context.Background())

	events, err := r.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// notify sends synchronously under the lock before Put/Delete returns, so
	// the events are queued deterministically by the time these calls return.
	_ = r.Put(ctx, &MCPServer{ID: "a"})
	_ = r.Delete(ctx, "a")

	if e := <-events; e.Type != RegistryPut || e.ID != "a" {
		t.Errorf("event 1 = %+v, want put/a", e)
	}
	if e := <-events; e.Type != RegistryDelete || e.ID != "a" {
		t.Errorf("event 2 = %+v, want delete/a", e)
	}

	// Canceling closes the channel.
	cancel()
	for range events { //nolint:revive // draining until close
	}
}

// memoryRegistry returns live pointers, so the Manager's in-place status updates
// are visible through Get (the behavior a serializing impl will replace with
// read-modify-Put in Slice 2). This documents/locks that contract.
func TestMemoryRegistry_ReturnsLivePointer(t *testing.T) {
	r := NewMemoryRegistry()
	ctx := context.Background()
	_ = r.Put(ctx, &MCPServer{ID: "s", Status: StatusUnknown})

	got, _ := r.Get(ctx, "s")
	got.Status = StatusHealthy // in-place mutation

	again, _ := r.Get(ctx, "s")
	if again.Status != StatusHealthy {
		t.Error("memoryRegistry.Get should return the live pointer (in-place update visible)")
	}
}
