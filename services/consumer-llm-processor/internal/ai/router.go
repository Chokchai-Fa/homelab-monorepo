package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"consumer-llm-processor/internal/store"
)

// Tier is a class the classifier assigns to each question.
type Tier string

const (
	TierSimple    Tier = "simple"
	TierGeneral   Tier = "general"
	TierTechnical Tier = "technical"
	TierImage     Tier = "image"    // user asks to generate a picture
	TierReminder  Tier = "reminder" // user asks to set a reminder

	classifyTimeout = 10 * time.Second
)

// Result is what the router hands back: an answer text, or a generated
// image (with optional caption text) when the user asked for a picture.
// ReminderIntent means the message belongs to consumer-reminder; this
// service stays a pure router, so the caller republishes the request there
// and no chain is consulted.
type Result struct {
	Text           string
	ImageData      []byte
	ImageMime      string
	ReminderIntent bool
}

// Router picks a provider chain per question difficulty and falls back to the
// next provider in the chain on any error (rate limits included), which is
// what keeps the whole pipeline inside free tiers.
type Router struct {
	classifier Provider
	chains     map[Tier][]Provider
	vision     []Provider
	imageGen   ImageGenerator
}

// NewRouter builds a router. classifier may be nil, in which case every
// question uses the general chain. Empty simple/technical chains fall back
// to general. vision is the chain used whenever a request carries an image
// - difficulty tiering doesn't apply there since free-tier vision-capable
// models are scarce and picking one able to see the image matters more than
// picking one suited to the question's difficulty. imageGen handles
// "draw me a ..." requests; nil sends those to the general chain, which
// answers in text.
func NewRouter(classifier Provider, simple, general, technical, vision []Provider, imageGen ImageGenerator) *Router {
	chains := map[Tier][]Provider{TierGeneral: general}
	chains[TierSimple] = simple
	if len(simple) == 0 {
		chains[TierSimple] = general
	}
	chains[TierTechnical] = technical
	if len(technical) == 0 {
		chains[TierTechnical] = general
	}
	return &Router{classifier: classifier, chains: chains, vision: vision, imageGen: imageGen}
}

// Route answers one request. An attached input image goes straight to the
// vision chain; an image-generation ask goes to the image generator;
// everything else is classified and tried against the tier's chain until a
// provider answers.
func (r *Router) Route(ctx context.Context, history []store.Message, userMessage string, image *Image) (Result, error) {
	label := "vision"
	chain := r.vision
	if image == nil {
		tier := r.classify(ctx, userMessage)
		if tier == TierReminder {
			log.Info().Str("tier", string(TierReminder)).Msg("route: reminder intent - handing off")
			return Result{ReminderIntent: true}, nil
		}
		if tier == TierImage && r.imageGen != nil {
			return r.generateImage(ctx, userMessage)
		}
		if tier == TierImage {
			tier = TierGeneral // no generator configured: answer in text
		}
		label = string(tier)
		chain = r.chains[tier]
	}

	for _, p := range chain {
		answer, err := p.Reply(ctx, history, userMessage, image)
		if err != nil {
			log.Warn().Str("tier", label).Str("provider", p.Name()).Err(err).Msg("route: provider failed - trying next")
			continue
		}
		log.Info().Str("tier", label).Str("provider", p.Name()).Msg("route: answered")
		return Result{Text: answer}, nil
	}
	return Result{}, fmt.Errorf("all providers failed for tier %q", label)
}

// RouteStream is the streaming counterpart of Route: it classifies the
// message, then streams the tier chain's answer through emit token-by-token,
// returning the full text in Result. Provider fallback is preserved but
// constrained: once any token has been emitted to the caller the answer is
// committed to that provider, so a provider that fails mid-stream returns the
// error instead of silently restarting on another model. A provider that
// fails before emitting anything (e.g. an immediate 429) still falls back.
//
// Images and image-generation are not supported on the streaming path (the
// web channel is text-only); a reminder intent short-circuits like Route.
func (r *Router) RouteStream(ctx context.Context, history []store.Message, userMessage string, emit func(delta string) error) (Result, error) {
	tier := r.classify(ctx, userMessage)
	if tier == TierReminder {
		return Result{ReminderIntent: true}, nil
	}
	// No generator on this path: an image ask is answered as text.
	if tier == TierImage {
		tier = TierGeneral
	}
	chain := r.chains[tier]
	label := string(tier)

	var emitted int
	guardedEmit := func(delta string) error {
		emitted += len(delta)
		return emit(delta)
	}

	var lastErr error
	for _, p := range chain {
		full, err := streamOne(ctx, p, history, userMessage, guardedEmit)
		if err == nil {
			log.Info().Str("tier", label).Str("provider", p.Name()).Bool("streamed", isStream(p)).Msg("route: streamed answer")
			return Result{Text: full}, nil
		}
		lastErr = err
		if emitted > 0 {
			// Already streamed partial output - can't switch providers now.
			log.Error().Str("tier", label).Str("provider", p.Name()).Err(err).Msg("route: stream failed after partial output - aborting")
			return Result{}, err
		}
		log.Warn().Str("tier", label).Str("provider", p.Name()).Err(err).Msg("route: stream provider failed before output - trying next")
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no providers configured for tier %q", label)
	}
	return Result{}, fmt.Errorf("all providers failed for tier %q: %w", label, lastErr)
}

// streamOne streams a single provider's answer. A StreamProvider streams
// natively; any other Provider is called once and its whole answer emitted as
// a single delta, so the streaming path works with every provider.
func streamOne(ctx context.Context, p Provider, history []store.Message, userMessage string, emit func(delta string) error) (string, error) {
	if sp, ok := p.(StreamProvider); ok {
		return sp.ReplyStream(ctx, history, userMessage, nil, emit)
	}
	full, err := p.Reply(ctx, history, userMessage, nil)
	if err != nil {
		return "", err
	}
	if err := emit(full); err != nil {
		return full, err
	}
	return full, nil
}

func isStream(p Provider) bool {
	_, ok := p.(StreamProvider)
	return ok
}

func (r *Router) generateImage(ctx context.Context, prompt string) (Result, error) {
	start := time.Now()
	data, mime, caption, err := r.imageGen.Generate(ctx, prompt)
	if err != nil {
		log.Error().Str("generator", r.imageGen.Name()).Err(err).Msg("route: image generation failed")
		return Result{}, err
	}
	log.Info().Str("tier", string(TierImage)).Str("provider", r.imageGen.Name()).Dur("duration", time.Since(start)).Int("bytes", len(data)).Msg("route: image generated")
	return Result{Text: strings.TrimSpace(caption), ImageData: data, ImageMime: mime}, nil
}

// classify asks the small classifier model for a one-word tier, defaulting
// to general on any failure so a broken classifier never blocks replies.
func (r *Router) classify(ctx context.Context, userMessage string) Tier {
	if r.classifier == nil {
		return TierGeneral
	}
	cctx, cancel := context.WithTimeout(ctx, classifyTimeout)
	defer cancel()

	verdict, err := r.classifier.Reply(cctx, nil, userMessage, nil)
	if err != nil {
		log.Warn().Str("classifier", r.classifier.Name()).Err(err).Msg("route: classify failed - defaulting to general")
		return TierGeneral
	}
	switch v := strings.ToLower(strings.TrimSpace(verdict)); {
	case strings.Contains(v, "reminder"):
		return TierReminder
	case strings.Contains(v, "technical"):
		return TierTechnical
	case strings.Contains(v, "image"):
		return TierImage
	case strings.Contains(v, "simple"):
		return TierSimple
	default:
		return TierGeneral
	}
}
