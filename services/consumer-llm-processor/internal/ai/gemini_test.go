package ai

import (
	"testing"

	"google.golang.org/genai"
)

// These tests exercise Gemini's pure struct logic (Name, Derive) without
// touching the network: New() dials a real genai.Client, so it is left
// untested here and covered instead by the router/provider fakes.

func TestGeminiName(t *testing.T) {
	g := &Gemini{name: "gemini/gemini-3-flash"}
	if got, want := g.Name(), "gemini/gemini-3-flash"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestGeminiDeriveSharesClientAndModel(t *testing.T) {
	base := &Gemini{
		name:     "gemini/base",
		model:    "gemini-3-flash",
		system:   PersonaInstruction,
		thinking: genai.ThinkingLevelLow,
	}

	derived := base.Derive("gemini/classifier", ClassifierInstruction, false)

	if derived.client != base.client {
		t.Error("Derive should share the underlying client")
	}
	if derived.model != base.model {
		t.Errorf("model = %q, want %q (shared)", derived.model, base.model)
	}
	if derived.name != "gemini/classifier" {
		t.Errorf("name = %q, want gemini/classifier", derived.name)
	}
	if derived.system != ClassifierInstruction {
		t.Errorf("system = %q, want classifier instruction", derived.system)
	}
	if derived.thinking != genai.ThinkingLevelLow {
		t.Errorf("thinking = %v, want Low for deepThinking=false", derived.thinking)
	}
}

func TestGeminiDeriveDeepThinking(t *testing.T) {
	base := &Gemini{name: "gemini/base", model: "m", system: "s", thinking: genai.ThinkingLevelLow}

	derived := base.Derive("gemini/technical", "deep system", true)

	if derived.thinking != genai.ThinkingLevelHigh {
		t.Errorf("thinking = %v, want High for deepThinking=true", derived.thinking)
	}
}
