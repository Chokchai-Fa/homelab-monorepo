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
