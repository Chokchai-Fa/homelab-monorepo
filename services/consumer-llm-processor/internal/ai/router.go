package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"consumer-llm-processor/internal/store"
)

// Tier is a difficulty class the classifier assigns to each question.
type Tier string

const (
	TierSimple    Tier = "simple"
	TierGeneral   Tier = "general"
	TierTechnical Tier = "technical"

	classifyTimeout = 10 * time.Second
)

// Router picks a provider chain per question difficulty and falls back to the
// next provider in the chain on any error (rate limits included), which is
// what keeps the whole pipeline inside free tiers.
type Router struct {
	classifier Provider
	chains     map[Tier][]Provider
}

// NewRouter builds a router. classifier may be nil, in which case every
// question uses the general chain. Empty chains fall back to general.
func NewRouter(classifier Provider, simple, general, technical []Provider) *Router {
	chains := map[Tier][]Provider{TierGeneral: general}
	chains[TierSimple] = simple
	if len(simple) == 0 {
		chains[TierSimple] = general
	}
	chains[TierTechnical] = technical
	if len(technical) == 0 {
		chains[TierTechnical] = general
	}
	return &Router{classifier: classifier, chains: chains}
}

func (r *Router) Name() string { return "router" }

// Reply classifies the question, then tries each provider in the tier's
// chain until one answers.
func (r *Router) Reply(ctx context.Context, history []store.Message, userMessage string) (string, error) {
	tier := r.classify(ctx, userMessage)
	for _, p := range r.chains[tier] {
		answer, err := p.Reply(ctx, history, userMessage)
		if err != nil {
			log.Warn().Str("tier", string(tier)).Str("provider", p.Name()).Err(err).Msg("route: provider failed - trying next")
			continue
		}
		log.Info().Str("tier", string(tier)).Str("provider", p.Name()).Msg("route: answered")
		return answer, nil
	}
	return "", fmt.Errorf("all providers failed for tier %q", tier)
}

// classify asks the small classifier model for a one-word difficulty tier,
// defaulting to general on any failure so a broken classifier never blocks
// replies.
func (r *Router) classify(ctx context.Context, userMessage string) Tier {
	if r.classifier == nil {
		return TierGeneral
	}
	cctx, cancel := context.WithTimeout(ctx, classifyTimeout)
	defer cancel()

	verdict, err := r.classifier.Reply(cctx, nil, userMessage)
	if err != nil {
		log.Warn().Str("classifier", r.classifier.Name()).Err(err).Msg("route: classify failed - defaulting to general")
		return TierGeneral
	}
	switch v := strings.ToLower(strings.TrimSpace(verdict)); {
	case strings.Contains(v, "technical"):
		return TierTechnical
	case strings.Contains(v, "simple"):
		return TierSimple
	default:
		return TierGeneral
	}
}
