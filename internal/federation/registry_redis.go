package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

const (
	// redisServerPrefix namespaces server-definition keys.
	redisServerPrefix = "federation:servers:"
	// redisEventChannel carries put/delete notifications across replicas.
	redisEventChannel = "federation:events"
	// redisScanBatch is the SCAN COUNT hint when listing definitions.
	redisScanBatch = 100
)

// redisRegistry is a shared Registry backed by Redis: server definitions are
// JSON under federation:servers:<id>, and put/delete are announced on a pub/sub
// channel so every gateway replica can converge. This is what makes the
// federated set consistent across replicas (issue #19). Live MCPService
// connections are still built per-pod (lazily, on demand) from these shared
// definitions.
type redisRegistry struct {
	rdb *redis.Client
}

// NewRedisRegistry returns a Registry backed by the given Redis client.
func NewRedisRegistry(rdb *redis.Client) Registry {
	return &redisRegistry{rdb: rdb}
}

// NewRedisRegistryFromURL builds a Redis client from a redis:// URL and returns
// a Registry over it.
func NewRedisRegistryFromURL(url string) (Registry, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return NewRedisRegistry(redis.NewClient(opt)), nil
}

func (*redisRegistry) key(id string) string { return redisServerPrefix + id }

func (r *redisRegistry) Put(ctx context.Context, server *MCPServer) error {
	if server == nil {
		return errors.New("server cannot be nil")
	}
	if server.ID == "" {
		return errors.New("server ID cannot be empty")
	}
	data, err := json.Marshal(server)
	if err != nil {
		return fmt.Errorf("marshal server: %w", err)
	}
	if err := r.rdb.Set(ctx, r.key(server.ID), data, 0).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	r.publish(ctx, RegistryPut, server.ID)
	return nil
}

func (r *redisRegistry) Get(ctx context.Context, id string) (*MCPServer, error) {
	data, err := r.rdb.Get(ctx, r.key(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrServerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var s MCPServer
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal server: %w", err)
	}
	return &s, nil
}

func (r *redisRegistry) Delete(ctx context.Context, id string) error {
	n, err := r.rdb.Del(ctx, r.key(id)).Result()
	if err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	if n == 0 {
		return ErrServerNotFound
	}
	r.publish(ctx, RegistryDelete, id)
	return nil
}

func (r *redisRegistry) List(ctx context.Context) ([]*MCPServer, error) {
	var keys []string
	iter := r.rdb.Scan(ctx, 0, redisServerPrefix+"*", redisScanBatch).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	vals, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis mget: %w", err)
	}
	servers := make([]*MCPServer, 0, len(vals))
	for _, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue // key vanished between SCAN and MGET
		}
		var srv MCPServer
		if err := json.Unmarshal([]byte(s), &srv); err != nil {
			continue
		}
		servers = append(servers, &srv)
	}
	return servers, nil
}

func (r *redisRegistry) Watch(ctx context.Context) (<-chan RegistryEvent, error) {
	pubsub := r.rdb.Subscribe(ctx, redisEventChannel)
	out := make(chan RegistryEvent, watchChannelBuffer)

	go func() {
		defer close(out)
		defer func() { _ = pubsub.Close() }()
		in := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-in:
				if !ok {
					return
				}
				evt, ok := parseRegistryEvent(msg.Payload)
				if !ok {
					continue
				}
				select {
				case out <- evt:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// publish is best-effort: a dropped notification is healed by the next reconcile
// (operator drift loop) or by lazy self-heal on invoke.
func (r *redisRegistry) publish(ctx context.Context, t RegistryEventType, id string) {
	_ = r.rdb.Publish(ctx, redisEventChannel, string(t)+":"+id).Err()
}

// parseRegistryEvent decodes a "<type>:<id>" pub/sub payload. The type prefix
// contains no colon, so splitting on the first ':' correctly preserves ids that
// contain colons.
func parseRegistryEvent(payload string) (RegistryEvent, bool) {
	i := strings.IndexByte(payload, ':')
	if i < 0 {
		return RegistryEvent{}, false
	}
	t := RegistryEventType(payload[:i])
	if t != RegistryPut && t != RegistryDelete {
		return RegistryEvent{}, false
	}
	return RegistryEvent{Type: t, ID: payload[i+1:]}, true
}
