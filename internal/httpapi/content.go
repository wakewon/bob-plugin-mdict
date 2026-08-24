package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// etagFor derives a strong ETag from the served bytes.
func etagFor(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// newByteSeeker adapts a byte slice to the ReadSeeker http.ServeContent needs,
// which is what gives resource responses Range support for free.
func newByteSeeker(data []byte) io.ReadSeeker { return bytes.NewReader(data) }
