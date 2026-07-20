package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// rewriteTransport redirects every request to target, so CloudflareImage's
// hardcoded api.cloudflare.com URL can be exercised against an httptest
// server without touching production code.
type rewriteTransport struct {
	target *url.URL
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func newTestCloudflareImage(t *testing.T, ts *httptest.Server) *CloudflareImage {
	t.Helper()
	target, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	c := NewCloudflareImage("acct123", "token123", "@cf/flux-1-schnell")
	c.client = &http.Client{Transport: rewriteTransport{target: target}}
	return c
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func TestCloudflareImageName(t *testing.T) {
	c := NewCloudflareImage("acct", "token", "flux")
	if got, want := c.Name(), "cloudflare-image/flux"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestCloudflareImageGenerateJSONBase64Response(t *testing.T) {
	png := testPNG(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cfImageResponse{Success: true}
		resp.Result.Image = base64.StdEncoding.EncodeToString(png)
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := newTestCloudflareImage(t, ts)
	data, mime, caption, err := c.Generate(context.Background(), "a cat")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg (re-encoded)", mime)
	}
	if caption != "" {
		t.Errorf("caption = %q, want empty (workers ai has none)", caption)
	}
	if len(data) == 0 {
		t.Error("expected non-empty image data")
	}
}

func TestCloudflareImageGenerateRawImageResponse(t *testing.T) {
	png := testPNG(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}))
	defer ts.Close()

	c := newTestCloudflareImage(t, ts)
	data, mime, _, err := c.Generate(context.Background(), "a dog")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg", mime)
	}
	if len(data) == 0 {
		t.Error("expected non-empty image data")
	}
}

func TestCloudflareImageGenerateAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cfImageResponse{Success: false}
		resp.Errors = []struct {
			Message string `json:"message"`
		}{{Message: "quota exceeded"}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := newTestCloudflareImage(t, ts)
	_, _, _, err := c.Generate(context.Background(), "a cat")
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("err = %v, want it to mention quota exceeded", err)
	}
}

func TestCloudflareImageGenerateNoImageNoErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(cfImageResponse{Success: false})
	}))
	defer ts.Close()

	c := newTestCloudflareImage(t, ts)
	_, _, _, err := c.Generate(context.Background(), "a cat")
	if err == nil || !strings.Contains(err.Error(), "no image in response") {
		t.Fatalf("err = %v, want 'no image in response'", err)
	}
}

func TestCloudflareImageGenerateNonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer ts.Close()

	c := newTestCloudflareImage(t, ts)
	_, _, _, err := c.Generate(context.Background(), "a cat")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want it to mention status 500", err)
	}
}

func TestCloudflareImageGenerateInvalidBase64(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"result":{"image":"not-valid-base64!!!"}}`))
	}))
	defer ts.Close()

	c := newTestCloudflareImage(t, ts)
	_, _, _, err := c.Generate(context.Background(), "a cat")
	if err == nil {
		t.Fatal("expected decode error for invalid base64")
	}
}

func TestCloudflareImageGeneratePromptTruncatedTo2048(t *testing.T) {
	var gotPrompt string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotPrompt = body.Prompt
		json.NewEncoder(w).Encode(cfImageResponse{Success: false})
	}))
	defer ts.Close()

	c := newTestCloudflareImage(t, ts)
	longPrompt := strings.Repeat("a", 3000)
	c.Generate(context.Background(), longPrompt)

	if len(gotPrompt) != 2048 {
		t.Errorf("prompt length = %d, want 2048", len(gotPrompt))
	}
}
