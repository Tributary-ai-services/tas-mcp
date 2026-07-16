// Package reduction adapts Gatekeeper's content extractor into the narrow
// Reducer port that the federation layer's reduce-at-source ResultProcessor
// depends on. Keeping the Gatekeeper import here (rather than in the federation
// package) keeps the federation hot path and its tests free of the cross-module
// dependency and the external SLM/embedder services.
//
// The returned *Reducer satisfies federation.Reducer structurally (a
// Reduce(ctx, content, query) (string, error) method), so the federation
// package never imports this one — no cycle, and federation stays dependency-
// light.
package reduction

import (
	"context"
	"fmt"

	"github.com/Tributary-ai-services/Gatekeeper/pkg/extract"
)

// Config configures the Gatekeeper-backed reducer. It mirrors tas-llm-router's
// extraction config so both services tune the same extractor the same way.
type Config struct {
	// Enabled turns reduce-at-source on. When false, New returns (nil, nil) and
	// the caller leaves the federation Manager on its no-op processor.
	Enabled bool

	// EmbedModel / OllamaURL configure the embedder used by the relevance step
	// (Extract with a query). Embeddings always run on Ollama.
	EmbedModel string
	OllamaURL  string

	// MinContentSize is the byte threshold below which the extractor does no
	// work — small tool results aren't worth reducing.
	MinContentSize int

	// SLM* configure the optional summarization step (query-less Summarize, and
	// the rewrite half of Extract). Provider "ollama" needs a local GPU;
	// "openai"/"anthropic" route to a cloud model. Off → relevance-only.
	SLMEnabled   bool
	SLMProvider  string
	SLMBaseURL   string
	SLMModel     string
	SLMAPIKey    string
	SLMMaxTokens int

	// MaxOutputBytes caps the reduced size of a single tool-result block
	// (Extract only). 0 = extractor default.
	MaxOutputBytes int
}

// Reducer reduces a single tool-result text block via Gatekeeper's extractor.
type Reducer struct {
	ex             extract.Extractor
	hasEmbed       bool
	hasSLM         bool
	maxOutputBytes int
}

// New builds a Reducer from cfg. It returns (nil, nil) when reduction is
// disabled so the caller can simply skip installing a processor. A non-nil
// error means the extractor could not be constructed; the caller should treat
// that as "reduction unavailable" and leave the no-op processor in place
// (reduce-at-source must never take a service down).
func New(cfg Config) (*Reducer, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Relevance-only reduction (Extract) needs a per-request query to rank
	// against, but the MCP tool-call path does not yet thread one to the
	// reducer — so without an SLM the reducer has no working reduction mode and
	// would silently no-op every result. Refuse rather than pretend to reduce.
	// (When the tool-call query is threaded through, relax this to allow
	// relevance-only mode.)
	if !cfg.SLMEnabled {
		return nil, fmt.Errorf("reduce-at-source requires an SLM (set SLMEnabled/REDUCTION_SLM_ENABLED): " +
			"relevance-only reduction needs a per-request query the MCP tool-call path does not yet provide")
	}

	ec := extract.DefaultExtractorConfig()
	ec.EnableEmbedding = true
	if cfg.EmbedModel != "" {
		ec.Embedding.Model = cfg.EmbedModel
	}
	ec.Embedding.URL = cfg.OllamaURL
	if cfg.MinContentSize > 0 {
		ec.MinContentSize = cfg.MinContentSize
	}

	ec.EnableSLM = cfg.SLMEnabled
	if cfg.SLMEnabled {
		ec.SLM = extract.SLMConfig{
			Provider:  cfg.SLMProvider,
			URL:       cfg.SLMBaseURL,
			Model:     cfg.SLMModel,
			APIKey:    cfg.SLMAPIKey,
			MaxTokens: cfg.SLMMaxTokens,
		}
		// A local SLM shares the Ollama endpoint when no base URL is given.
		if cfg.SLMProvider == extract.SLMProviderOllama && cfg.SLMBaseURL == "" {
			ec.SLM.URL = cfg.OllamaURL
		}
	}

	ex, err := extract.NewExtractor(ec)
	if err != nil {
		return nil, fmt.Errorf("build extractor: %w", err)
	}

	return &Reducer{
		ex:             ex,
		hasEmbed:       ec.EnableEmbedding,
		hasSLM:         cfg.SLMEnabled,
		maxOutputBytes: cfg.MaxOutputBytes,
	}, nil
}

// Reduce shrinks a single tool-result text block:
//
//   - With a query and embeddings, it runs relevance extraction (Extract) —
//     deterministic (same content+query ⇒ same retained chunks), so the reduced
//     form is byte-stable and cache-safe.
//   - Without a query, it falls back to SLM summarization (Summarize, map-reduce)
//     when an SLM is configured.
//   - With neither applicable, it returns the content unchanged (a no-op is not
//     an error — there is simply nothing to reduce with).
//
// A non-nil error means the reduction attempt failed; the federation processor
// treats that as fail-open and keeps the original content.
func (r *Reducer) Reduce(ctx context.Context, content, query string) (string, error) {
	if r == nil || r.ex == nil {
		return content, nil
	}

	if query != "" && r.hasEmbed {
		res, err := r.ex.Extract(ctx, extract.ExtractRequest{
			Content:     []byte(content),
			Query:       query,
			ContentType: "text",
			MaxOutput:   r.maxOutputBytes,
		})
		if err != nil {
			return "", err
		}
		return string(res.Content), nil
	}

	if r.hasSLM {
		res, err := r.ex.Summarize(ctx, extract.SummarizeRequest{
			Content:     []byte(content),
			Strategy:    extract.StrategyMapReduce,
			ContentType: "text",
		})
		if err != nil {
			return "", err
		}
		return res.Summary, nil
	}

	return content, nil
}

// Close releases the underlying extractor's resources.
func (r *Reducer) Close() error {
	if r == nil || r.ex == nil {
		return nil
	}
	return r.ex.Close()
}
