package ai

import "testing"

// TestPortfolioPersonaContainsKeyFacts guards the facts visitors have
// actually asked about: the correct Thai spelling of the name (the LLM used
// to guess "ช็อคชัย") and the InCIT 2025 paper (previously answered "not
// found"). If someone trims the prompt, this fails loudly.
func TestPortfolioPersonaContainsKeyFacts(t *testing.T) {
	facts := []string{
		"โชคชัย ฟ้ารุ่งสาง", // exact Thai name
		"GitCoFL",           // research paper
		"InCIT 2025",        // conference
		"LINE Corporation",  // current role
	}
	for _, f := range facts {
		if !contains(PortfolioPersonaInstruction, f) {
			t.Errorf("PortfolioPersonaInstruction is missing required fact %q", f)
		}
	}
}

// contains is a tiny substring check kept local so the test has no imports
// beyond testing.
func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
