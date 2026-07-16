package reduction

import (
	"context"
	"errors"
	"testing"

	"github.com/Tributary-ai-services/Gatekeeper/pkg/extract"
)

// fakeExtractor records which method was called and returns canned results, so
// the Reducer's routing logic can be tested without Ollama or an SLM.
type fakeExtractor struct {
	extractCalled   bool
	summarizeCalled bool
	extractOut      string
	summarizeOut    string
	err             error
}

func (f *fakeExtractor) Extract(_ context.Context, _ extract.ExtractRequest) (*extract.ExtractResult, error) {
	f.extractCalled = true
	if f.err != nil {
		return nil, f.err
	}
	return &extract.ExtractResult{Content: []byte(f.extractOut)}, nil
}

func (f *fakeExtractor) Summarize(_ context.Context, _ extract.SummarizeRequest) (*extract.SummarizeResult, error) {
	f.summarizeCalled = true
	if f.err != nil {
		return nil, f.err
	}
	return &extract.SummarizeResult{Summary: f.summarizeOut}, nil
}

func (f *fakeExtractor) Close() error { return nil }

func newTestReducer(ex extract.Extractor, hasEmbed, hasSLM bool) *Reducer {
	return &Reducer{ex: ex, hasEmbed: hasEmbed, hasSLM: hasSLM}
}

func TestReduce_WithQueryUsesExtract(t *testing.T) {
	f := &fakeExtractor{extractOut: "short"}
	r := newTestReducer(f, true, true)

	out, err := r.Reduce(context.Background(), "a long tool result", "the query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.extractCalled || f.summarizeCalled {
		t.Errorf("query present should route to Extract only: extract=%v summarize=%v", f.extractCalled, f.summarizeCalled)
	}
	if out != "short" {
		t.Errorf("out = %q, want %q", out, "short")
	}
}

func TestReduce_NoQueryUsesSummarize(t *testing.T) {
	f := &fakeExtractor{summarizeOut: "tl;dr"}
	r := newTestReducer(f, true, true)

	out, err := r.Reduce(context.Background(), "a long tool result", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.extractCalled || !f.summarizeCalled {
		t.Errorf("no query should route to Summarize only: extract=%v summarize=%v", f.extractCalled, f.summarizeCalled)
	}
	if out != "tl;dr" {
		t.Errorf("out = %q, want %q", out, "tl;dr")
	}
}

func TestReduce_QueryButNoEmbedFallsBackToSummarize(t *testing.T) {
	f := &fakeExtractor{summarizeOut: "tl;dr"}
	r := newTestReducer(f, false, true) // embeddings off

	_, err := r.Reduce(context.Background(), "content", "a query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.extractCalled || !f.summarizeCalled {
		t.Errorf("no embeddings should skip Extract and use Summarize: extract=%v summarize=%v", f.extractCalled, f.summarizeCalled)
	}
}

func TestReduce_NoEmbedNoSLMIsNoop(t *testing.T) {
	f := &fakeExtractor{}
	r := newTestReducer(f, false, false)

	out, err := r.Reduce(context.Background(), "content", "a query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.extractCalled || f.summarizeCalled {
		t.Error("with neither embeddings nor SLM, Reduce must be a no-op")
	}
	if out != "content" {
		t.Errorf("no-op must return content unchanged, got %q", out)
	}
}

func TestReduce_ExtractErrorPropagates(t *testing.T) {
	f := &fakeExtractor{err: errors.New("embedder down")}
	r := newTestReducer(f, true, true)

	if _, err := r.Reduce(context.Background(), "content", "a query"); err == nil {
		t.Error("Extract error should propagate (processor fails open on it)")
	}
}

func TestReduce_SummarizeErrorPropagates(t *testing.T) {
	f := &fakeExtractor{err: errors.New("slm down")}
	r := newTestReducer(f, true, true)

	if _, err := r.Reduce(context.Background(), "content", ""); err == nil {
		t.Error("Summarize error should propagate")
	}
}

func TestReduce_NilReducerIsNoop(t *testing.T) {
	var r *Reducer
	out, err := r.Reduce(context.Background(), "content", "q")
	if err != nil || out != "content" {
		t.Errorf("nil reducer must be a safe no-op: out=%q err=%v", out, err)
	}
}

func TestNew_DisabledReturnsNil(t *testing.T) {
	r, err := New(Config{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != nil {
		t.Error("disabled config should return a nil reducer so the caller skips installing a processor")
	}
}

func TestNew_EnabledBuildsReducer(t *testing.T) {
	// A build with SLM enabled should succeed without contacting Ollama (the
	// extractor connects lazily on first use).
	r, err := New(Config{Enabled: true, OllamaURL: "http://ollama:11434", SLMEnabled: true})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if r == nil {
		t.Fatal("enabled config should return a reducer")
	}
	if !r.hasEmbed || !r.hasSLM {
		t.Errorf("enabled+SLM config: hasEmbed=%v hasSLM=%v, want true/true", r.hasEmbed, r.hasSLM)
	}
	_ = r.Close()
}

func TestNew_EnabledWithoutSLMErrors(t *testing.T) {
	// Relevance-only (no SLM) can't reduce without a per-request query, so New
	// must surface a config error rather than return a silently no-op reducer.
	r, err := New(Config{Enabled: true, OllamaURL: "http://ollama:11434"})
	if err == nil {
		t.Error("enabled without SLM should error (would otherwise silently no-op)")
	}
	if r != nil {
		t.Error("no reducer should be returned on the config error")
	}
}
