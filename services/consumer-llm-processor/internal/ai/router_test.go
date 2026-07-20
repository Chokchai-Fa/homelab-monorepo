package ai

import (
	"context"
	"errors"
	"testing"

	"consumer-llm-processor/internal/store"
)

type fakeProvider struct {
	name   string
	answer string
	err    error
	calls  int
	images []*Image
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Reply(_ context.Context, _ []store.Message, _ string, image *Image) (string, error) {
	f.calls++
	f.images = append(f.images, image)
	return f.answer, f.err
}

// fakeStreamProvider streams a sequence of deltas, optionally failing after
// emitting `emitBeforeErr` of them. It also satisfies Provider.Reply.
type fakeStreamProvider struct {
	name          string
	deltas        []string
	err           error
	emitBeforeErr int
	calls         int
}

func (f *fakeStreamProvider) Name() string { return f.name }

func (f *fakeStreamProvider) Reply(_ context.Context, _ []store.Message, _ string, _ *Image) (string, error) {
	full := ""
	for _, d := range f.deltas {
		full += d
	}
	return full, f.err
}

func (f *fakeStreamProvider) ReplyStream(_ context.Context, _ []store.Message, _ string, _ *Image, emit func(string) error) (string, error) {
	f.calls++
	full := ""
	for i, d := range f.deltas {
		if f.err != nil && i >= f.emitBeforeErr {
			return full, f.err
		}
		full += d
		if e := emit(d); e != nil {
			return full, e
		}
	}
	if f.err != nil {
		return full, f.err
	}
	return full, nil
}

func TestRouteStreamEmitsDeltasAndReturnsFull(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "general"}
	sp := &fakeStreamProvider{name: "g", deltas: []string{"He ", "works ", "at LINE."}}
	r := newTestRouter(classifier, nil, []Provider{sp}, nil, nil, nil)

	var got string
	res, err := r.RouteStream(context.Background(), nil, "where?", func(d string) error {
		got += d
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "He works at LINE." || res.Text != got {
		t.Fatalf("emitted %q, result %q", got, res.Text)
	}
}

func TestRouteStreamFallsBackBeforeFirstToken(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "general"}
	failing := &fakeStreamProvider{name: "first", deltas: []string{"x"}, err: errors.New("429"), emitBeforeErr: 0}
	ok := &fakeStreamProvider{name: "second", deltas: []string{"ok ", "answer"}}
	r := newTestRouter(classifier, nil, []Provider{failing, ok}, nil, nil, nil)

	var got string
	res, err := r.RouteStream(context.Background(), nil, "hi", func(d string) error {
		got += d
		return nil
	})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got %v", err)
	}
	if res.Text != "ok answer" || got != "ok answer" {
		t.Fatalf("expected second provider's answer, got %q / %q", res.Text, got)
	}
	if ok.calls != 1 {
		t.Fatalf("expected fallback provider to be called once, got %d", ok.calls)
	}
}

func TestRouteStreamNoFallbackAfterPartialOutput(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "general"}
	// Emits one delta, then fails - the router must NOT try the next provider.
	failing := &fakeStreamProvider{name: "first", deltas: []string{"partial ", "more"}, err: errors.New("boom"), emitBeforeErr: 1}
	next := &fakeStreamProvider{name: "second", deltas: []string{"should not run"}}
	r := newTestRouter(classifier, nil, []Provider{failing, next}, nil, nil, nil)

	var got string
	_, err := r.RouteStream(context.Background(), nil, "hi", func(d string) error {
		got += d
		return nil
	})
	if err == nil {
		t.Fatal("expected error after partial output")
	}
	if got != "partial " {
		t.Fatalf("expected only the partial delta, got %q", got)
	}
	if next.calls != 0 {
		t.Fatal("must not fall back after streaming partial output")
	}
}

func TestRouteStreamNonStreamProviderEmitsWhole(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "general"}
	plain := &fakeProvider{name: "plain", answer: "whole answer"}
	r := newTestRouter(classifier, nil, []Provider{plain}, nil, nil, nil)

	var got string
	res, err := r.RouteStream(context.Background(), nil, "hi", func(d string) error {
		got += d
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "whole answer" || res.Text != "whole answer" {
		t.Fatalf("expected whole answer emitted once, got %q / %q", got, res.Text)
	}
}

func TestRouteStreamReminderShortCircuits(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "reminder"}
	sp := &fakeStreamProvider{name: "g", deltas: []string{"nope"}}
	r := newTestRouter(classifier, nil, []Provider{sp}, nil, nil, nil)

	res, err := r.RouteStream(context.Background(), nil, "remind me", func(string) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.ReminderIntent {
		t.Fatal("expected reminder intent")
	}
	if sp.calls != 0 {
		t.Fatal("providers must not run on a reminder intent")
	}
}

type fakeImageGen struct {
	data    []byte
	caption string
	err     error
	calls   int
	prompts []string
}

func (f *fakeImageGen) Name() string { return "fake-imagegen" }

func (f *fakeImageGen) Generate(_ context.Context, prompt string) ([]byte, string, string, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	return f.data, "image/jpeg", f.caption, f.err
}

func newTestRouter(classifier Provider, simple, general, technical, vision []Provider, gen ImageGenerator) *Router {
	return NewRouter(classifier, simple, general, technical, vision, gen)
}

func TestRouterRoutesByTier(t *testing.T) {
	cases := []struct {
		verdict string
		want    string
	}{
		{"simple", "simple-answer"},
		{"general", "general-answer"},
		{"technical", "technical-answer"},
		{"Technical.", "technical-answer"}, // sloppy classifier output
		{"gibberish", "general-answer"},    // unknown verdict defaults to general
	}
	for _, tc := range cases {
		classifier := &fakeProvider{name: "classifier", answer: tc.verdict}
		r := newTestRouter(classifier,
			[]Provider{&fakeProvider{name: "s", answer: "simple-answer"}},
			[]Provider{&fakeProvider{name: "g", answer: "general-answer"}},
			[]Provider{&fakeProvider{name: "t", answer: "technical-answer"}},
			nil, nil,
		)
		got, err := r.Route(context.Background(), nil, "hello", nil)
		if err != nil {
			t.Fatalf("verdict %q: unexpected error: %v", tc.verdict, err)
		}
		if got.Text != tc.want {
			t.Errorf("verdict %q: got %q, want %q", tc.verdict, got.Text, tc.want)
		}
	}
}

func TestRouterReminderIntentShortCircuits(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "reminder"}
	general := &fakeProvider{name: "g", answer: "general-answer"}
	r := newTestRouter(classifier, nil, []Provider{general}, nil, nil, nil)

	got, err := r.Route(context.Background(), nil, "เตือนพรุ่งนี้ 9 โมง กินยา", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.ReminderIntent {
		t.Fatal("expected ReminderIntent to be set")
	}
	if got.Text != "" {
		t.Errorf("reminder intent must not produce chat text, got %q", got.Text)
	}
}

func TestRouterFallsBackOnProviderError(t *testing.T) {
	broken := &fakeProvider{name: "broken", err: errors.New("status 429")}
	backup := &fakeProvider{name: "backup", answer: "backup-answer"}
	r := newTestRouter(&fakeProvider{name: "classifier", answer: "general"},
		nil, []Provider{broken, backup}, nil, nil, nil)

	got, err := r.Route(context.Background(), nil, "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "backup-answer" {
		t.Errorf("got %q, want backup-answer", got.Text)
	}
	if broken.calls != 1 || backup.calls != 1 {
		t.Errorf("calls: broken=%d backup=%d, want 1 and 1", broken.calls, backup.calls)
	}
}

func TestRouterAllProvidersFail(t *testing.T) {
	r := newTestRouter(nil, nil,
		[]Provider{&fakeProvider{name: "broken", err: errors.New("down")}}, nil, nil, nil)
	if _, err := r.Route(context.Background(), nil, "hello", nil); err == nil {
		t.Fatal("expected error when every provider fails")
	}
}

func TestRouterClassifierFailureDefaultsToGeneral(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", err: errors.New("down")}
	general := &fakeProvider{name: "g", answer: "general-answer"}
	r := newTestRouter(classifier,
		[]Provider{&fakeProvider{name: "s", answer: "simple-answer"}},
		[]Provider{general},
		[]Provider{&fakeProvider{name: "t", answer: "technical-answer"}},
		nil, nil,
	)
	got, err := r.Route(context.Background(), nil, "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "general-answer" {
		t.Errorf("got %q, want general-answer", got.Text)
	}
}

func TestRouterEmptyTierChainsFallBackToGeneral(t *testing.T) {
	general := &fakeProvider{name: "g", answer: "general-answer"}
	r := newTestRouter(&fakeProvider{name: "classifier", answer: "technical"},
		nil, []Provider{general}, nil, nil, nil)
	got, err := r.Route(context.Background(), nil, "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "general-answer" {
		t.Errorf("got %q, want general-answer", got.Text)
	}
}

func TestRouterRoutesImagesToVisionChainBypassingClassifier(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "general"}
	general := &fakeProvider{name: "g", answer: "general-answer"}
	vision := &fakeProvider{name: "v", answer: "vision-answer"}
	r := newTestRouter(classifier, nil, []Provider{general}, nil, []Provider{vision}, nil)

	img := &Image{Data: []byte("fake"), MimeType: "image/jpeg"}
	got, err := r.Route(context.Background(), nil, "what is this?", img)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "vision-answer" {
		t.Errorf("got %q, want vision-answer", got.Text)
	}
	if classifier.calls != 0 {
		t.Errorf("classifier should be skipped for image requests, got %d calls", classifier.calls)
	}
	if general.calls != 0 {
		t.Errorf("general chain should not be used for image requests, got %d calls", general.calls)
	}
	if len(vision.images) != 1 || vision.images[0] != img {
		t.Errorf("vision provider did not receive the image: %+v", vision.images)
	}
}

func TestRouterVisionChainAllFailReturnsError(t *testing.T) {
	r := newTestRouter(nil, nil, []Provider{&fakeProvider{name: "g", answer: "general-answer"}}, nil, nil, nil)
	img := &Image{Data: []byte("fake"), MimeType: "image/jpeg"}
	if _, err := r.Route(context.Background(), nil, "what is this?", img); err == nil {
		t.Fatal("expected error when no vision provider is configured")
	}
}

func TestRouterImageTierUsesGenerator(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "image"}
	general := &fakeProvider{name: "g", answer: "general-answer"}
	gen := &fakeImageGen{data: []byte("jpeg-bytes"), caption: "a cat"}
	r := newTestRouter(classifier, nil, []Provider{general}, nil, nil, gen)

	got, err := r.Route(context.Background(), nil, "draw a cat", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got.ImageData) != "jpeg-bytes" || got.ImageMime != "image/jpeg" {
		t.Errorf("image result = %+v", got)
	}
	if got.Text != "a cat" {
		t.Errorf("caption = %q, want 'a cat'", got.Text)
	}
	if gen.calls != 1 || gen.prompts[0] != "draw a cat" {
		t.Errorf("generator calls = %d, prompts = %v", gen.calls, gen.prompts)
	}
	if general.calls != 0 {
		t.Errorf("general chain should not run for image tier, got %d calls", general.calls)
	}
}

func TestRouterImageTierWithoutGeneratorFallsBackToGeneral(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "image"}
	general := &fakeProvider{name: "g", answer: "general-answer"}
	r := newTestRouter(classifier, nil, []Provider{general}, nil, nil, nil)

	got, err := r.Route(context.Background(), nil, "draw a cat", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "general-answer" || got.ImageData != nil {
		t.Errorf("got %+v, want general text answer", got)
	}
}

func TestRouterImageGeneratorFailureReturnsError(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "image"}
	gen := &fakeImageGen{err: errors.New("quota exceeded")}
	r := newTestRouter(classifier, nil, []Provider{&fakeProvider{name: "g", answer: "x"}}, nil, nil, gen)

	if _, err := r.Route(context.Background(), nil, "draw a cat", nil); err == nil {
		t.Fatal("expected error when generator fails")
	}
}
