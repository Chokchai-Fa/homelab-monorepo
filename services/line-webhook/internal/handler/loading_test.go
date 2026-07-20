package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestShowLoadingAnimationGuards(t *testing.T) {
	// None of these may attempt HTTP: point the endpoint at a server that
	// fails the test if hit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("guarded showLoadingAnimation must not call the LINE API")
	}))
	defer srv.Close()
	old := loadingEndpoint
	loadingEndpoint = srv.URL
	defer func() { loadingEndpoint = old }()

	(&LineHandler{}).showLoadingAnimation("u1")                                // nil cfg
	(&LineHandler{cfg: &Config{}}).showLoadingAnimation("u1")                  // no channel token
	(&LineHandler{cfg: &Config{ChannelToken: "tok"}}).showLoadingAnimation("") // no user ID
	time.Sleep(50 * time.Millisecond)                                          // give a stray goroutine time to surface
}

func TestStartLoadingAnimationSendsChatIDAndSeconds(t *testing.T) {
	type loadingReq struct {
		ChatID         string `json:"chatId"`
		LoadingSeconds int    `json:"loadingSeconds"`
	}
	var (
		mu   sync.Mutex
		got  loadingReq
		auth string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		auth = r.Header.Get("Authorization")
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("request body is not valid JSON: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	old := loadingEndpoint
	loadingEndpoint = srv.URL
	defer func() { loadingEndpoint = old }()

	h := &LineHandler{cfg: &Config{ChannelToken: "tok"}}
	if err := h.startLoadingAnimation("u1"); err != nil {
		t.Fatalf("startLoadingAnimation() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer tok")
	}
	if got.ChatID != "u1" {
		t.Errorf("chatId = %q, want %q", got.ChatID, "u1")
	}
	if got.LoadingSeconds != loadingSeconds {
		t.Errorf("loadingSeconds = %d, want %d", got.LoadingSeconds, loadingSeconds)
	}
	if got.LoadingSeconds%5 != 0 || got.LoadingSeconds < 5 || got.LoadingSeconds > 60 {
		t.Errorf("loadingSeconds = %d, must be a multiple of 5 in [5,60] per the LINE API", got.LoadingSeconds)
	}
}

func TestStartLoadingAnimationNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"invalid chatId"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	old := loadingEndpoint
	loadingEndpoint = srv.URL
	defer func() { loadingEndpoint = old }()

	h := &LineHandler{cfg: &Config{ChannelToken: "tok"}}
	if err := h.startLoadingAnimation("not-a-user"); err == nil {
		t.Fatal("startLoadingAnimation() on 400 = nil, want error")
	}
}

func TestPublishAIRequestTriggersLoadingAnimation(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		close(done)
	}))
	defer srv.Close()
	old := loadingEndpoint
	loadingEndpoint = srv.URL
	defer func() { loadingEndpoint = old }()

	pub := &fakePublisher{}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai", ChannelToken: "tok"}, pub: pub}
	event, _ := textEvent("/ai hello")
	if err := h.publishAIRequest(event, "hello"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishAIRequest did not trigger the loading indicator")
	}
	if len(pub.aiRequests) != 1 {
		t.Fatalf("AI request not published alongside loading indicator: %+v", pub.aiRequests)
	}
}
