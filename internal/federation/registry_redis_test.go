package federation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const (
	gitEndpoint = "http://git:3000"
	watchID     = "s1"
)

func newTestRedisRegistry(t *testing.T) Registry {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewRedisRegistry(rdb)
}

func TestRedisRegistry_PutGetListDelete(t *testing.T) {
	r := newTestRedisRegistry(t)
	ctx := context.Background()

	if _, err := r.Get(ctx, "missing"); !errors.Is(err, ErrServerNotFound) {
		t.Errorf("Get missing: want ErrServerNotFound, got %v", err)
	}

	s := &MCPServer{ID: "git", Name: "Git", Endpoint: gitEndpoint, Protocol: ProtocolHTTP, Capabilities: []string{"log"}}
	if err := r.Put(ctx, s); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := r.Get(ctx, "git")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != s.Name || got.Endpoint != gitEndpoint || got.Protocol != ProtocolHTTP || len(got.Capabilities) != 1 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}

	_ = r.Put(ctx, &MCPServer{ID: "db"})
	all, err := r.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List = %d, want 2", len(all))
	}

	if err := r.Delete(ctx, "git"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(ctx, "git"); !errors.Is(err, ErrServerNotFound) {
		t.Error("git should be gone after Delete")
	}
	if err := r.Delete(ctx, "git"); !errors.Is(err, ErrServerNotFound) {
		t.Errorf("Delete missing: want ErrServerNotFound, got %v", err)
	}
}

func TestRedisRegistry_PutValidation(t *testing.T) {
	r := newTestRedisRegistry(t)
	if err := r.Put(context.Background(), nil); err == nil {
		t.Error("Put(nil) should error")
	}
	if err := r.Put(context.Background(), &MCPServer{ID: ""}); err == nil {
		t.Error("Put empty id should error")
	}
}

func TestRedisRegistry_Watch(t *testing.T) {
	r := newTestRedisRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := r.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	// Let the subscription establish before publishing (pub/sub drops messages
	// sent before SUBSCRIBE is active).
	time.Sleep(150 * time.Millisecond)

	_ = r.Put(ctx, &MCPServer{ID: watchID})
	_ = r.Delete(ctx, watchID)

	got := readEvents(t, events, 2, 3*time.Second)
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(got), got)
	}
	if got[0].Type != RegistryPut || got[0].ID != watchID {
		t.Errorf("event[0] = %+v, want put/s1", got[0])
	}
	if got[1].Type != RegistryDelete || got[1].ID != watchID {
		t.Errorf("event[1] = %+v, want delete/s1", got[1])
	}
}

func TestParseRegistryEvent(t *testing.T) {
	cases := []struct {
		payload string
		want    RegistryEvent
		ok      bool
	}{
		{"put:git", RegistryEvent{RegistryPut, "git"}, true},
		{"delete:db", RegistryEvent{RegistryDelete, "db"}, true},
		{"put:has:colon", RegistryEvent{RegistryPut, "has:colon"}, true}, // id keeps later colons
		{"bogus:x", RegistryEvent{}, false},
		{"noseparator", RegistryEvent{}, false},
	}
	for _, c := range cases {
		got, ok := parseRegistryEvent(c.payload)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseRegistryEvent(%q) = %+v,%v; want %+v,%v", c.payload, got, ok, c.want, c.ok)
		}
	}
}

func readEvents(t *testing.T, ch <-chan RegistryEvent, n int, timeout time.Duration) []RegistryEvent {
	t.Helper()
	var out []RegistryEvent
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-deadline:
			return out
		}
	}
	return out
}
