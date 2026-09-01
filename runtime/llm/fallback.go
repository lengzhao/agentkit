package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
)

type fallbackMode string

const (
	fallbackOnRetryable fallbackMode = "retryable"
	fallbackOnQuota     fallbackMode = "quota"
	fallbackOnAny       fallbackMode = "any"
)

type FallbackConfig struct {
	// Models is the full ordered model chain when sharing one provider or pairing with deps.fallbacks.
	Models []string `json:"models,omitempty"`
	// FallbackModels are tried after the request model (from agent config) on the shared provider.
	FallbackModels []string `json:"fallbackModels,omitempty"`
	// FallbackOn selects which errors trigger failover: retryable (default), quota, or any.
	FallbackOn string `json:"fallbackOn,omitempty"`
}

type FallbackDeps struct {
	Provider  agentkit.LLMProvider   `json:"provider,omitempty"`
	Fallbacks []agentkit.LLMProvider `json:"fallbacks,omitempty"`
}

type fallbackTarget struct {
	provider agentkit.LLMProvider
	model    string
}

type Fallback struct {
	providers  []agentkit.LLMProvider
	models     []string
	fallbackOn fallbackMode
	useReqModel bool
}

// NewFallback registers llm/fallback: try alternate models or providers when the primary LLM call fails.
func NewFallback(cfg FallbackConfig, deps FallbackDeps) (agentkit.LLMProvider, error) {
	providers := flattenProviders(deps)
	if len(providers) == 0 {
		return nil, fmt.Errorf("llm/fallback requires deps.provider or deps.fallbacks")
	}

	mode, err := parseFallbackMode(cfg.FallbackOn)
	if err != nil {
		return nil, err
	}

	useReqModel := len(cfg.Models) == 0
	models := append([]string(nil), cfg.Models...)
	if useReqModel {
		models = append(models, cfg.FallbackModels...)
	}
	if len(models) == 0 && len(providers) == 1 {
		return nil, fmt.Errorf("llm/fallback requires config.models or config.fallbackModels")
	}
	if len(providers) > 1 && len(models) == 0 {
		return nil, fmt.Errorf("llm/fallback: multi-provider mode requires config.models")
	}

	return &Fallback{
		providers:   providers,
		models:      models,
		fallbackOn:  mode,
		useReqModel: useReqModel,
	}, nil
}

func flattenProviders(deps FallbackDeps) []agentkit.LLMProvider {
	out := make([]agentkit.LLMProvider, 0, 1+len(deps.Fallbacks))
	if deps.Provider != nil {
		out = append(out, deps.Provider)
	}
	out = append(out, deps.Fallbacks...)
	return out
}

func parseFallbackMode(raw string) (fallbackMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "retryable":
		return fallbackOnRetryable, nil
	case "quota":
		return fallbackOnQuota, nil
	case "any":
		return fallbackOnAny, nil
	default:
		return "", fmt.Errorf("llm/fallback: unsupported fallbackOn %q", raw)
	}
}

func (f *Fallback) Name() string { return "fallback" }

func (f *Fallback) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	targets := f.buildTargets(req.Model)
	if len(targets) == 0 {
		return nil, fmt.Errorf("llm/fallback: empty target chain")
	}

	var lastErr error
	for i, target := range targets {
		attemptReq := req
		attemptReq.Model = target.model
		stream, err := target.provider.Stream(ctx, attemptReq)
		if err == nil {
			if i > 0 {
				slog.Info("llm fallback: switched provider/model",
					"provider", target.provider.Name(),
					"model", target.model,
					"attempt", i+1,
				)
			}
			return &fallbackStream{
				ctx:        ctx,
				req:        attemptReq,
				targets:    targets[i:],
				fallbackOn: f.fallbackOn,
				inner:      stream,
			}, nil
		}
		lastErr = err
		if !shouldFallback(err, f.fallbackOn) || i == len(targets)-1 {
			return nil, err
		}
		slog.Warn("llm fallback: stream open failed, trying next",
			"provider", target.provider.Name(),
			"model", target.model,
			"attempt", i+1,
			"error", err,
		)
	}
	return nil, lastErr
}

func (f *Fallback) buildTargets(reqModel string) []fallbackTarget {
	providers := f.providers
	models := f.models
	if f.useReqModel && strings.TrimSpace(reqModel) != "" {
		models = append([]string{reqModel}, models...)
	}
	models = dedupeStrings(models)

	count := len(models)
	if count == 0 {
		count = len(providers)
	}
	if count == 0 {
		return nil
	}
	if len(providers) == 1 {
		providers = repeatProvider(providers[0], count)
	} else if len(providers) < count {
		last := providers[len(providers)-1]
		for len(providers) < count {
			providers = append(providers, last)
		}
	}

	targets := make([]fallbackTarget, count)
	for i := 0; i < count; i++ {
		model := ""
		if i < len(models) {
			model = models[i]
		}
		targets[i] = fallbackTarget{
			provider: providers[min(i, len(providers)-1)],
			model:    model,
		}
	}
	return targets
}

type fallbackStream struct {
	ctx        context.Context
	req        agentkit.LLMRequest
	targets    []fallbackTarget
	fallbackOn fallbackMode
	inner      agentkit.LLMStream
	delivered  bool
	targetIdx  int
}

func (s *fallbackStream) Recv() (agentkit.LLMEvent, error) {
	for {
		if s.inner == nil {
			if err := s.openNextTarget(); err != nil {
				return agentkit.LLMEvent{}, err
			}
		}

		ev, err := s.inner.Recv()
		if err == nil {
			s.delivered = true
			return ev, nil
		}
		if errors.Is(err, io.EOF) {
			return ev, err
		}
		if s.delivered || s.targetIdx >= len(s.targets)-1 || !shouldFallback(err, s.fallbackOn) {
			return ev, err
		}
		if cerr := s.ctx.Err(); cerr != nil {
			return ev, cerr
		}

		failed := s.targets[s.targetIdx]
		_ = s.inner.Close()
		s.inner = nil
		s.targetIdx++
		next := s.targets[s.targetIdx]
		slog.Warn("llm fallback: stream recv failed before output, trying next",
			"from_provider", failed.provider.Name(),
			"from_model", failed.model,
			"to_provider", next.provider.Name(),
			"to_model", next.model,
			"error", err,
		)
	}
}

func (s *fallbackStream) openNextTarget() error {
	for s.targetIdx < len(s.targets) {
		target := s.targets[s.targetIdx]
		attemptReq := s.req
		attemptReq.Model = target.model
		stream, err := target.provider.Stream(s.ctx, attemptReq)
		if err == nil {
			if s.targetIdx > 0 {
				slog.Info("llm fallback: switched provider/model",
					"provider", target.provider.Name(),
					"model", target.model,
					"attempt", s.targetIdx+1,
				)
			}
			s.req = attemptReq
			s.inner = stream
			return nil
		}
		if !shouldFallback(err, s.fallbackOn) || s.targetIdx >= len(s.targets)-1 {
			return err
		}
		slog.Warn("llm fallback: stream open failed, trying next",
			"provider", target.provider.Name(),
			"model", target.model,
			"attempt", s.targetIdx+1,
			"error", err,
		)
		s.targetIdx++
	}
	return io.EOF
}

func (s *fallbackStream) Close() error {
	if s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

func shouldFallback(err error, mode fallbackMode) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	switch mode {
	case fallbackOnQuota:
		return IsQuotaError(err)
	case fallbackOnAny:
		return true
	default:
		return IsRetryableError(err)
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func repeatProvider(provider agentkit.LLMProvider, count int) []agentkit.LLMProvider {
	out := make([]agentkit.LLMProvider, count)
	for i := range out {
		out[i] = provider
	}
	return out
}
