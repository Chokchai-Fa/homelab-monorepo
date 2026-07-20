package ai

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestGeminiImageName(t *testing.T) {
	g := &GeminiImage{model: "gemini-2.5-flash-image"}
	if got, want := g.Name(), "gemini-image/gemini-2.5-flash-image"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestReencodeJPEGConvertsValidImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			src.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("encode source png: %v", err)
	}

	out, err := reencodeJPEG(buf.Bytes())
	if err != nil {
		t.Fatalf("reencodeJPEG: %v", err)
	}
	if len(out) < 2 || out[0] != 0xFF || out[1] != 0xD8 {
		t.Errorf("output doesn't look like JPEG (magic bytes): % x", out[:min(len(out), 4)])
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("re-encoded bytes are not valid JPEG: %v", err)
	}
}

func TestReencodeJPEGRejectsInvalidData(t *testing.T) {
	_, err := reencodeJPEG([]byte("this is not an image"))
	if err == nil {
		t.Fatal("expected error decoding non-image data")
	}
}
