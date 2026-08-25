package validate_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/httpapi"
	"github.com/wakewon/bob-plugin-mdict/internal/mdrender"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
)

// The validation pipeline calls the service and the adapter directly. That is
// one step short of what a client actually receives, so this test closes the
// last gap in process: the same EntrySet is asked for over HTTP, and what
// comes back has to be the same thing.
//
// No daemon is started and Bob is never involved. An httptest server is the
// real handler.

func lookupOverHTTP(t *testing.T, handler http.Handler, body map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v2/lookup", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	// The service refuses anything that did not come from loopback, which a
	// synthetic request has to state for itself.
	request.RemoteAddr = "127.0.0.1:54321"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v2/lookup = %d: %s", recorder.Code, recorder.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	return decoded
}

func TestHTTPSerializationPreservesTheSemanticResult(t *testing.T) {
	entries := bilingualEntries(24)
	svc := newService(t, map[string][]testmdx.Entry{"cn": entries})
	handler := httpapi.New(svc, slog.New(slog.NewTextHandler(os.Stderr,
		&slog.HandlerOptions{Level: slog.LevelError}))).Handler()

	key := entries[0].Key
	direct, err := svc.Lookup(key, service.LookupOptions{Mode: service.ModeExact, MaxExamples: 8})
	if err != nil {
		t.Fatal(err)
	}
	set := direct.Matches[0].EntrySet()

	response := lookupOverHTTP(t, handler, map[string]any{
		"query": key, "format": "bob", "maxExamples": 8,
	})

	// The Bob structure the wire carries must equal the one the adapter makes
	// from the same EntrySet. Comparing the decoded forms rather than the bytes
	// keeps this a test of content, not of field ordering.
	opts := bobadapter.DefaultOptions()
	opts.MaxExamplesPerSense = 8
	expected := bobadapter.RenderEntrySet(set, opts)
	if !sameJSON(t, expected, response["bob"]) {
		t.Errorf("the Bob result differs over HTTP\nwire: %#v", response["bob"])
	}

	// And the canonical IR must survive serialization intact, because it is the
	// contract every other client reads.
	var wire struct {
		Matches []struct {
			LookupKey string                `json:"lookupKey"`
			Records   []entryir.EntryRecord `json:"records"`
		} `json:"matches"`
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Matches) == 0 {
		t.Fatal("no matches came back over HTTP")
	}
	if wire.Matches[0].LookupKey != set.LookupKey {
		t.Errorf("lookup key %q over HTTP, %q in process", wire.Matches[0].LookupKey, set.LookupKey)
	}
	if len(wire.Matches[0].Records) != len(set.Records) {
		t.Fatalf("%d records over HTTP, %d in process", len(wire.Matches[0].Records), len(set.Records))
	}

	// The Markdown renderer reads the IR, so an IR that survived the wire must
	// render the same way on either side of it.
	roundTripped := &entryir.EntrySet{
		LookupKey: wire.Matches[0].LookupKey,
		Headword:  set.Headword,
		Records:   wire.Matches[0].Records,
	}
	if mdrender.RenderEntrySet(roundTripped, mdrender.DefaultOptions()) !=
		mdrender.RenderEntrySet(set, mdrender.DefaultOptions()) {
		t.Error("the Markdown rendering differs after a round trip through the API")
	}
}

func sameJSON(t *testing.T, left, right any) bool {
	t.Helper()
	leftBytes, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(leftBytes, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rightBytes, &b); err != nil {
		t.Fatal(err)
	}
	return string(mustMarshal(t, a)) == string(mustMarshal(t, b))
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
