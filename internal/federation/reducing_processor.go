package federation

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// maxReduceCacheEntries bounds the reduce-once cache so a long-lived gateway
// federating high-cardinality tool output can't grow it without limit. Reducing
// is deterministic, so evicting an entry only costs a recompute, never
// correctness.
const maxReduceCacheEntries = 4096

// Reducer deterministically shrinks a single block of tool-result text — the
// port the reducing ResultProcessor depends on. The Gatekeeper-backed adapter
// (extract.Extractor) is wired in behind this interface (Slice 2b), which keeps
// the cross-module dependency out of the federation hot path and lets the
// processor be tested with a fake.
//
// Contract:
//   - Deterministic: identical (content, query) MUST yield identical output, so
//     the reduced form is byte-stable across turns and prompt caching survives.
//   - Content-only shrink: never inject or reorder — the result must remain a
//     faithful, smaller rendering of the same tool output.
//   - An error means "could not reduce"; the caller keeps the original text
//     (fail-open). Returning the input unchanged with a nil error is also valid
//     (nothing to reduce).
type Reducer interface {
	Reduce(ctx context.Context, content, query string) (reduced string, err error)
}

// reducingResultProcessor reduces the text blocks of a tools/call result at the
// federation boundary — the cache-safe "reduce at the source, once" seam (see
// result_processor.go for why here and not at the LLM gateway).
//
// It is installed via Manager.SetResultProcessor. Until it is, the Manager runs
// the noopResultProcessor and reduction stays OFF.
type reducingResultProcessor struct {
	reducer Reducer

	// cache memoizes reduced text by content hash so identical tool output isn't
	// re-reduced on repeat calls (the "reduce once" half of the contract). The
	// Reducer is deterministic, so a cache hit returns the exact same bytes a
	// fresh reduction would — caching only saves the work, it never changes the
	// result. Bounded (LRU) so it can't grow without limit.
	cache *reduceLRU
}

// NewReducingResultProcessor returns a ResultProcessor that reduces tools/call
// result text through r. A nil reducer yields a processor that passes every
// response through unchanged (defensive — callers should install the no-op
// processor instead of a nil-backed one).
func NewReducingResultProcessor(r Reducer) ResultProcessor {
	return &reducingResultProcessor{
		reducer: r,
		cache:   newReduceLRU(maxReduceCacheEntries),
	}
}

// ProcessResult reduces the text content of a tools/call response and returns
// it. For any other method, a nil reducer, an unrecognized result shape, or a
// per-block reduction error, it returns resp unchanged (fail-open — a reduction
// failure must never drop or corrupt a tool result).
func (p *reducingResultProcessor) ProcessResult(ctx context.Context, method string, resp *MCPResponse) *MCPResponse {
	if p.reducer == nil || resp == nil || resp.Error != nil {
		return resp
	}
	// Only tools/call responses carry reducible external content.
	if method != "tools/call" {
		return resp
	}

	query := toolCallQuery(resp)
	var savedBytes int

	// A bare-string result is held by value on resp, so reduce it directly.
	if s, ok := resp.Result.(string); ok && s != "" {
		if reduced := p.reduceOnce(ctx, s, query); len(reduced) < len(s) {
			resp.Result = reduced
			savedBytes += len(s) - len(reduced)
		}
	} else {
		for _, b := range textBlocks(resp.Result) {
			orig := b.text()
			if orig == "" {
				continue
			}
			reduced := p.reduceOnce(ctx, orig, query)
			if len(reduced) < len(orig) {
				b.set(reduced)
				savedBytes += len(orig) - len(reduced)
			}
		}
	}

	if savedBytes > 0 {
		if resp.Meta == nil {
			resp.Meta = map[string]interface{}{}
		}
		resp.Meta["reduced"] = true
		resp.Meta["reduced_bytes_saved"] = savedBytes
	}
	return resp
}

// reduceOnce returns the reduced form of content, computing it at most once per
// distinct (query, content) and memoizing the result. On a reducer error it
// returns the original content unchanged (fail-open) and does not cache.
func (p *reducingResultProcessor) reduceOnce(ctx context.Context, content, query string) string {
	key := reduceKey(query, content)

	if cached, ok := p.cache.get(key); ok {
		return cached
	}

	reduced, err := p.reducer.Reduce(ctx, content, query)
	if err != nil {
		// Fail-open: keep the original text, don't poison the cache.
		return content
	}

	p.cache.put(key, reduced)
	return reduced
}

// reduceLRU is a small mutex-guarded LRU mapping content-hash keys to reduced
// text. It bounds the reduce-once memoization to max entries, evicting the
// least-recently-used key on overflow.
type reduceLRU struct {
	mu  sync.Mutex
	max int
	ll  *list.List // front = most recently used
	m   map[string]*list.Element
}

type reduceEntry struct{ key, val string }

func newReduceLRU(max int) *reduceLRU {
	if max <= 0 {
		max = maxReduceCacheEntries
	}
	return &reduceLRU{max: max, ll: list.New(), m: make(map[string]*list.Element, max)}
}

func (c *reduceLRU) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*reduceEntry).val, true
	}
	return "", false
}

func (c *reduceLRU) put(key, val string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		c.ll.MoveToFront(el)
		el.Value.(*reduceEntry).val = val
		return
	}
	c.m[key] = c.ll.PushFront(&reduceEntry{key: key, val: val})
	if c.ll.Len() > c.max {
		if oldest := c.ll.Back(); oldest != nil {
			c.ll.Remove(oldest)
			delete(c.m, oldest.Value.(*reduceEntry).key)
		}
	}
}

func reduceKey(query, content string) string {
	h := sha256.New()
	h.Write([]byte(query))
	h.Write([]byte{0}) // domain separator so query|content can't collide
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// textBlock is a handle to one editable text block inside an MCPResponse.Result,
// abstracting over the two shapes we see after JSON decoding: a bare string
// result, or the MCP content array [{type:"text", text:"..."}].
type textBlock struct {
	get func() string
	put func(string)
}

func (b textBlock) text() string { return b.get() }
func (b textBlock) set(s string) { b.put(s) }

// textBlocks returns the editable text blocks of a decoded MCP tools/call
// result object:
//
//	{"content": [{"type":"text","text":"..."}]} → one block per text element
//
// The bare-string result shape is handled directly by ProcessResult (the string
// is held by value on the response, so it can't be edited through a block). The
// map/slice here is held by reference, so put() mutates the live result. Unknown
// shapes yield no blocks (the response passes through untouched).
func textBlocks(result interface{}) []textBlock {
	if r, ok := result.(map[string]interface{}); ok {
		return contentArrayBlocks(r)
	}
	return nil
}

// contentArrayBlocks extracts text blocks from an MCP result object's "content"
// array. Each element that is {"type":"text","text": <string>} becomes an
// editable block backed by that element map.
func contentArrayBlocks(result map[string]interface{}) []textBlock {
	raw, ok := result["content"]
	if !ok {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var blocks []textBlock
	for _, it := range items {
		elem, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := elem["type"].(string); t != "text" {
			continue
		}
		if _, ok := elem["text"].(string); !ok {
			continue
		}
		e := elem // capture per-iteration map (held by reference)
		blocks = append(blocks, textBlock{
			get: func() string { s, _ := e["text"].(string); return s },
			put: func(s string) { e["text"] = s },
		})
	}
	return blocks
}

// toolCallQuery best-effort recovers a relevance query for the reduction from
// the response metadata. Absent an explicit query the reducer runs query-less
// (e.g. summarization rather than relevance extraction); returning "" is fine.
func toolCallQuery(resp *MCPResponse) string {
	if resp.Meta == nil {
		return ""
	}
	if q, ok := resp.Meta["query"].(string); ok {
		return q
	}
	return ""
}
