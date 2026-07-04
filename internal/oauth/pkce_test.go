package oauth

import (
	"strings"
	"testing"
)

func TestChallengeForRFC7636Vector(t *testing.T) {
	t.Parallel()
	// RFC 7636 Appendix B known-answer vector.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := challengeFor(verifier); got != want {
		t.Errorf("challengeFor = %q, want %q", got, want)
	}
}

func TestNewPKCE(t *testing.T) {
	t.Parallel()
	p, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	if p.Method != MethodS256 {
		t.Errorf("method = %q, want S256", p.Method)
	}
	if len(p.Verifier) != 43 { // 32 bytes -> 43 base64url chars, no padding
		t.Errorf("verifier len = %d, want 43", len(p.Verifier))
	}
	if strings.ContainsAny(p.Verifier, "=+/") {
		t.Errorf("verifier not URL-safe: %q", p.Verifier)
	}
	if p.Challenge != challengeFor(p.Verifier) {
		t.Error("challenge does not match verifier")
	}
}

func TestNewState(t *testing.T) {
	t.Parallel()
	a, _ := NewState()
	b, _ := NewState()
	if a == "" || a == b {
		t.Errorf("states should be non-empty and distinct: %q %q", a, b)
	}
}
