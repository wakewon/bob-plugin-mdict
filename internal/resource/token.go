// Package resource turns MDD references into opaque, tamper-proof URL tokens
// and serves the bytes behind them.
package resource

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrBadToken means the token was malformed, truncated, or not produced by
// this process.
var ErrBadToken = errors.New("invalid resource token")

// Ref is the payload a token carries.
type Ref struct {
	// DictionaryID scopes the reference. A token minted for one dictionary can
	// never address another.
	DictionaryID string `json:"d"`
	// ResourceRef is the MDD-internal key, e.g. "sound://synthetic/uk/example.mp3" in tests.
	// It is an index key, never a filesystem path.
	ResourceRef string `json:"r"`
}

// Tokenizer mints and verifies resource tokens.
//
// Tokens are AES-GCM sealed with a key generated fresh at every start, so they
// are opaque to the client, cannot be forged, and cannot be rewritten to point
// at a different dictionary or resource. Because a token decodes to a
// dictionary ID plus an MDD index key — and resolution only ever consults that
// dictionary's in-memory resource index — path traversal, symlink escape and
// arbitrary local file reads are structurally impossible rather than filtered.
type Tokenizer struct {
	aead cipher.AEAD
}

// NewTokenizer creates a tokenizer with a random per-process key.
func NewTokenizer() (*Tokenizer, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate token key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Tokenizer{aead: aead}, nil
}

// tokenEncoding writes tokens in lowercase base32: letters and the digits
// 2–7 only. A token travels inside Markdown links, and base64's "_" and "-"
// are not inert there — two underscores in a link on a line of several can
// be read as emphasis by a lenient renderer, which leaves the link unclickable.
var tokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Mint seals a reference into a token that is safe in URLs and Markdown.
func (t *Tokenizer) Mint(ref Ref) (string, error) {
	payload, err := json.Marshal(ref)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, t.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := t.aead.Seal(nonce, nonce, payload, nil)
	return strings.ToLower(tokenEncoding.EncodeToString(sealed)), nil
}

// Open verifies and decodes a token.
func (t *Tokenizer) Open(token string) (Ref, error) {
	var ref Ref
	raw, err := tokenEncoding.DecodeString(strings.ToUpper(token))
	// Exactly one spelling of a token is accepted — the one Mint wrote — so
	// an uppercased copy or one with stray trailing bits is not the same
	// token in disguise.
	if err != nil || strings.ToLower(tokenEncoding.EncodeToString(raw)) != token {
		return ref, ErrBadToken
	}
	nonceSize := t.aead.NonceSize()
	if len(raw) < nonceSize {
		return ref, ErrBadToken
	}
	payload, err := t.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return ref, ErrBadToken
	}
	if err := json.Unmarshal(payload, &ref); err != nil {
		return ref, ErrBadToken
	}
	if ref.DictionaryID == "" || ref.ResourceRef == "" {
		return ref, ErrBadToken
	}
	return ref, nil
}
