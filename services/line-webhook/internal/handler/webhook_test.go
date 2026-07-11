package handler

import "testing"

func TestIsAIRequest(t *testing.T) {
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}}

	cases := []struct {
		text string
		want bool
	}{
		{"/ai what is kubernetes?", true},
		{"/ai ถามอะไรก็ได้", true},
		{"/ai", true},
		{"  /ai reset  ", true},
		{"hello", false},
		{"/aid something", false},
		{"ai what is this", false},
		{"", false},
	}
	for _, c := range cases {
		if got := h.isAIRequest(c.text); got != c.want {
			t.Errorf("isAIRequest(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
