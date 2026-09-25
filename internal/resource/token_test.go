package resource

import (
	"strings"
	"testing"
)

func TestTokenRoundTrip(t *testing.T) {
	tokenizer, err := NewTokenizer()
	if err != nil {
		t.Fatal(err)
	}
	want := Ref{DictionaryID: "abc123", ResourceRef: `sound://synthetic/uk/example.mp3`}
	token, err := tokenizer.Mint(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tokenizer.Open(token)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// TestTokenIsOpaque checks that the token does not disclose what it points at.
func TestTokenIsOpaque(t *testing.T) {
	tokenizer, _ := NewTokenizer()
	token, _ := tokenizer.Mint(Ref{DictionaryID: "abc123", ResourceRef: "sound://synthetic/uk/secret.mp3"})
	for _, fragment := range []string{"abc123", "secret", "sound", "mp3"} {
		if strings.Contains(token, fragment) {
			t.Errorf("token %q leaks %q", token, fragment)
		}
	}
}

// TestForgedTokensAreRejected covers the traversal and cross-dictionary cases:
// a client cannot craft a token, nor edit one it was given.
func TestForgedTokensAreRejected(t *testing.T) {
	tokenizer, _ := NewTokenizer()
	valid, _ := tokenizer.Mint(Ref{DictionaryID: "abc123", ResourceRef: "sound://synthetic/uk/a.mp3"})

	bad := []string{
		"",
		"not-base64!!",
		"YWJj",
		valid + "x",
		"x" + valid,
		strings.ToUpper(valid),
		valid[:len(valid)-4],
		// A payload someone might hope is interpreted as a file path.
		"Li4vLi4vLi4vZXRjL3Bhc3N3ZA",
	}
	for _, token := range bad {
		if _, err := tokenizer.Open(token); err == nil {
			t.Errorf("Open(%q) succeeded, want rejection", token)
		}
	}
}

// TestTokensAreNotPortableBetweenProcesses documents that the signing key is
// per-process, so a token cannot outlive or escape the service that minted it.
func TestTokensAreNotPortableBetweenProcesses(t *testing.T) {
	first, _ := NewTokenizer()
	second, _ := NewTokenizer()
	token, _ := first.Mint(Ref{DictionaryID: "d", ResourceRef: "r"})
	if _, err := second.Open(token); err == nil {
		t.Error("a token minted by one tokenizer was accepted by another")
	}
}

func TestEmptyFieldsAreRejected(t *testing.T) {
	tokenizer, _ := NewTokenizer()
	token, _ := tokenizer.Mint(Ref{DictionaryID: "", ResourceRef: ""})
	if _, err := tokenizer.Open(token); err == nil {
		t.Error("a token with empty fields was accepted")
	}
}

// Tokens travel inside Markdown links, where "_" and "-" are not inert: two
// underscores in one link of several on a line can break it in a lenient
// renderer. Only lowercase letters and the digits 2–7 may appear.
func TestTokensAreMarkdownSafe(t *testing.T) {
	tokenizer, _ := NewTokenizer()
	for i := 0; i < 200; i++ {
		token, err := tokenizer.Mint(Ref{DictionaryID: "abc123", ResourceRef: "sound://synthetic/uk/a.mp3"})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range token {
			if !(r >= 'a' && r <= 'z') && !(r >= '2' && r <= '7') {
				t.Fatalf("token %q contains %q", token, r)
			}
		}
		if _, err := tokenizer.Open(token); err != nil {
			t.Fatalf("Open(%q): %v", token, err)
		}
	}
}
