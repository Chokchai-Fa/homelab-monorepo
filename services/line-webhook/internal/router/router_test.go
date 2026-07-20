package router

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"line-webhook/internal/handler"
	"line-webhook/internal/publisher"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// fakePublisher is a no-op EventPublisher, sufficient for exercising route
// wiring without a real NATS connection.
type fakePublisher struct{}

func (fakePublisher) PublishAIRequest(publisher.AIRequestEvent) error { return nil }
func (fakePublisher) PublishReply(publisher.ReplyEvent) error         { return nil }
func (fakePublisher) PublishPostback(publisher.PostbackEvent) error   { return nil }
func (fakePublisher) PublishProfile(publisher.ProfileEvent) error     { return nil }

type fakeGenImages struct {
	data map[string][]byte
}

func (f *fakeGenImages) GetGenerated(_ context.Context, id string) ([]byte, error) {
	data, ok := f.data[id]
	if !ok {
		return nil, errNotFound
	}
	return data, nil
}

type notFoundErr struct{}

func (notFoundErr) Error() string { return "not found" }

var errNotFound = notFoundErr{}

func TestNewRouterHealthCheck(t *testing.T) {
	e := NewRouter(RouterOptions{
		Config:    &handler.Config{ChannelSecret: "secret", AIPrefix: "/ai"},
		Publisher: fakePublisher{},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestNewRouterWebhookSignatureValidation(t *testing.T) {
	e := NewRouter(RouterOptions{
		Config:    &handler.Config{ChannelSecret: "secret", AIPrefix: "/ai"},
		Publisher: fakePublisher{},
	})

	body := []byte(`{"events":[]}`)

	// Missing signature header.
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /webhook without signature status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	// Invalid signature.
	req = httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Line-Signature", "bogus")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /webhook with bad signature status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// Valid signature reaches the handler, which accepts an empty event list.
	req = httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Line-Signature", sign("secret", body))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("POST /webhook with valid signature status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestNewRouterImagesRouteOptIn(t *testing.T) {
	// Without GenImages configured, /images/:id must not be registered.
	e := NewRouter(RouterOptions{
		Config:    &handler.Config{ChannelSecret: "secret", AIPrefix: "/ai"},
		Publisher: fakePublisher{},
	})
	req := httptest.NewRequest(http.MethodGet, "/images/abc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /images/abc without GenImages status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestNewRouterImagesRoute(t *testing.T) {
	gen := &fakeGenImages{data: map[string][]byte{"abc": []byte("jpeg-bytes")}}
	e := NewRouter(RouterOptions{
		Config:    &handler.Config{ChannelSecret: "secret", AIPrefix: "/ai"},
		Publisher: fakePublisher{},
		GenImages: gen,
	})

	req := httptest.NewRequest(http.MethodGet, "/images/abc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /images/abc status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "jpeg-bytes" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "jpeg-bytes")
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want %q", got, "image/jpeg")
	}

	req = httptest.NewRequest(http.MethodGet, "/images/missing", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /images/missing status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestNewRouterReusesProvidedEcho(t *testing.T) {
	e := echo.New()
	got := NewRouter(RouterOptions{
		Echo:      e,
		Config:    &handler.Config{ChannelSecret: "secret", AIPrefix: "/ai"},
		Publisher: fakePublisher{},
	})
	if got != e {
		t.Error("NewRouter() should reuse the provided *echo.Echo instance")
	}
}

func TestValidateSignatureMiddleware(t *testing.T) {
	cfg := &handler.Config{ChannelSecret: "top-secret"}
	mw := ValidateSignatureMiddleware(cfg)
	next := func(c echo.Context) error { return c.NoContent(http.StatusTeapot) }

	body := []byte(`{"hello":"world"}`)

	t.Run("missing header", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := mw(next)(c)
		httpErr, ok := err.(*echo.HTTPError)
		if !ok || httpErr.Code != http.StatusBadRequest {
			t.Fatalf("err = %v, want 400 HTTPError", err)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Line-Signature", "not-the-right-one")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := mw(next)(c)
		httpErr, ok := err.(*echo.HTTPError)
		if !ok || httpErr.Code != http.StatusUnauthorized {
			t.Fatalf("err = %v, want 401 HTTPError", err)
		}
	})

	t.Run("valid signature calls next and restores body", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Line-Signature", "  "+sign("top-secret", body)+"  ")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := mw(next)(c); err != nil {
			t.Fatalf("middleware error = %v", err)
		}
		if rec.Code != http.StatusTeapot {
			t.Fatalf("status = %d, want %d (next handler ran)", rec.Code, http.StatusTeapot)
		}

		// The body must be readable again by whatever runs after the
		// middleware (e.g. the actual webhook handler).
		restored, err := io.ReadAll(c.Request().Body)
		if err != nil {
			t.Fatalf("reading restored body: %v", err)
		}
		if string(restored) != string(body) {
			t.Errorf("restored body = %q, want %q", restored, body)
		}
	})
}
