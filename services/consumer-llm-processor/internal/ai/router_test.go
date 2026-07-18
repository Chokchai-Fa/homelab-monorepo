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
		r := NewRouter(classifier,
			[]Provider{&fakeProvider{name: "s", answer: "simple-answer"}},
			[]Provider{&fakeProvider{name: "g", answer: "general-answer"}},
			[]Provider{&fakeProvider{name: "t", answer: "technical-answer"}},
			nil,
		)
		got, err := r.Reply(context.Background(), nil, "hello", nil)
		if err != nil {
			t.Fatalf("verdict %q: unexpected error: %v", tc.verdict, err)
		}
		if got != tc.want {
			t.Errorf("verdict %q: got %q, want %q", tc.verdict, got, tc.want)
		}
	}
}

func TestRouterFallsBackOnProviderError(t *testing.T) {
	broken := &fakeProvider{name: "broken", err: errors.New("status 429")}
	backup := &fakeProvider{name: "backup", answer: "backup-answer"}
	r := NewRouter(&fakeProvider{name: "classifier", answer: "general"},
		nil, []Provider{broken, backup}, nil, nil)

	got, err := r.Reply(context.Background(), nil, "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "backup-answer" {
		t.Errorf("got %q, want backup-answer", got)
	}
	if broken.calls != 1 || backup.calls != 1 {
		t.Errorf("calls: broken=%d backup=%d, want 1 and 1", broken.calls, backup.calls)
	}
}

func TestRouterAllProvidersFail(t *testing.T) {
	r := NewRouter(nil, nil,
		[]Provider{&fakeProvider{name: "broken", err: errors.New("down")}}, nil, nil)
	if _, err := r.Reply(context.Background(), nil, "hello", nil); err == nil {
		t.Fatal("expected error when every provider fails")
	}
}

func TestRouterClassifierFailureDefaultsToGeneral(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", err: errors.New("down")}
	general := &fakeProvider{name: "g", answer: "general-answer"}
	r := NewRouter(classifier,
		[]Provider{&fakeProvider{name: "s", answer: "simple-answer"}},
		[]Provider{general},
		[]Provider{&fakeProvider{name: "t", answer: "technical-answer"}},
		nil,
	)
	got, err := r.Reply(context.Background(), nil, "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "general-answer" {
		t.Errorf("got %q, want general-answer", got)
	}
}

func TestRouterEmptyTierChainsFallBackToGeneral(t *testing.T) {
	general := &fakeProvider{name: "g", answer: "general-answer"}
	r := NewRouter(&fakeProvider{name: "classifier", answer: "technical"},
		nil, []Provider{general}, nil, nil)
	got, err := r.Reply(context.Background(), nil, "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "general-answer" {
		t.Errorf("got %q, want general-answer", got)
	}
}

func TestRouterRoutesImagesToVisionChainBypassingClassifier(t *testing.T) {
	classifier := &fakeProvider{name: "classifier", answer: "general"}
	general := &fakeProvider{name: "g", answer: "general-answer"}
	vision := &fakeProvider{name: "v", answer: "vision-answer"}
	r := NewRouter(classifier, nil, []Provider{general}, nil, []Provider{vision})

	img := &Image{Data: []byte("fake"), MimeType: "image/jpeg"}
	got, err := r.Reply(context.Background(), nil, "what is this?", img)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "vision-answer" {
		t.Errorf("got %q, want vision-answer", got)
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
	r := NewRouter(nil, nil, []Provider{&fakeProvider{name: "g", answer: "general-answer"}}, nil, nil)
	img := &Image{Data: []byte("fake"), MimeType: "image/jpeg"}
	if _, err := r.Reply(context.Background(), nil, "what is this?", img); err == nil {
		t.Fatal("expected error when no vision provider is configured")
	}
}
