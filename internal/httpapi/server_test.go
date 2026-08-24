package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/httpapi"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
	"github.com/wakewon/bob-plugin-mdict/internal/version"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	return newTestServerForDir(t, t.TempDir())
}

func newTestServerForDir(t *testing.T, dictionaryDir string) http.Handler {
	t.Helper()
	cfg := config.Config{DictionaryDir: dictionaryDir, CacheDir: t.TempDir(), Port: 15321}
	svc, err := service.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Rescan(); err != nil {
		t.Fatal(err)
	}
	return httpapi.New(svc, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))).Handler()
}

func TestDictionaryListIncludesUnavailableAndExplicitIDErrorsAreActionable(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "Broken", "Broken.mdx")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("synthetic invalid mdx"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := newTestServerForDir(t, root)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodGet, "/v2/dictionaries", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d", recorder.Code)
	}
	var list httpapi.DictionariesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Dictionaries) != 1 || list.Dictionaries[0].Health != "unavailable" {
		t.Fatalf("unavailable dictionary missing from list: %+v", list.Dictionaries)
	}

	for _, tc := range []struct {
		id   string
		code string
	}{
		{"expired-id", "dictionaryNotFound"},
		{list.Dictionaries[0].ID, "dictionaryUnavailable"},
	} {
		body := `{"query":"flimber","dictionaries":["` + tc.id + `"]}`
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(body)))
		var failure httpapi.ErrorResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &failure); err != nil {
			t.Fatal(err)
		}
		if failure.Error != tc.code || !strings.Contains(failure.Hint, "/list") {
			t.Errorf("ID %q error = %+v, want %s with /list hint", tc.id, failure, tc.code)
		}
	}
}

// newRequest builds a request that looks like it came from the loopback
// interface, which is the only source the service accepts.
func newRequest(method, path string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, path, body)
	request.RemoteAddr = "127.0.0.1:54321"
	return request
}

func TestStatusReportsContract(t *testing.T) {
	handler := newTestServer(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodGet, "/v2/status", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var payload httpapi.StatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	// The plugin refuses to talk to a service advertising a different API
	// version, so this field is a compatibility contract.
	if payload.APIVersion != version.APIVersion {
		t.Errorf("apiVersion = %q, want %q", payload.APIVersion, version.APIVersion)
	}
	if payload.ServiceVersion != version.Version {
		t.Errorf("serviceVersion = %q, want %q", payload.ServiceVersion, version.Version)
	}
	if payload.BuildCommit != version.Commit || payload.BuildCommit == "" {
		t.Errorf("buildCommit = %q, want non-empty %q", payload.BuildCommit, version.Commit)
	}
	if payload.Service != "bob-mdict" {
		t.Errorf("service = %q", payload.Service)
	}
	if payload.DictionaryCount != 0 {
		t.Errorf("dictionaryCount = %d, want 0 for an empty directory", payload.DictionaryCount)
	}
}

func TestLookupWithoutDictionariesExplainsWhatToDo(t *testing.T) {
	handler := newTestServer(t)
	recorder := httptest.NewRecorder()
	request := newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(`{"query":"hello"}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	var payload httpapi.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error != "noDictionaries" {
		t.Errorf("error code = %q", payload.Error)
	}
	// The user has to be told where to put their files, not just that it failed.
	if !strings.Contains(payload.Hint, ".mdx") || !strings.Contains(payload.Hint, "rescan") {
		t.Errorf("hint does not explain what to do: %q", payload.Hint)
	}
}

func TestLookupRejectsBadInput(t *testing.T) {
	handler := newTestServer(t)
	cases := []struct {
		name string
		body string
	}{
		{"not json", `not json at all`},
		{"missing query", `{}`},
		{"blank query", `{"query":"   "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(tc.body)))
			if recorder.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", recorder.Code)
			}
		})
	}
}

func TestV2LookupReturnsRecordsAndMultiRecordBobCard(t *testing.T) {
	root := t.TempDir()
	markup := func(pos, definition string) string {
		return `<article><h1>flimber</h1><div class="sense"><span class="pos">` + pos +
			`</span><span class="definition">` + definition + `</span></div></article>`
	}
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), []testmdx.Entry{
		{Key: "flimber", HTML: markup("noun", "synthetic noun definition")},
		{Key: "flimber", HTML: markup("verb", "synthetic verb definition")},
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestServerForDir(t, root)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup",
		strings.NewReader(`{"query":"flimber","format":"bob","limit":1}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Matches []map[string]json.RawMessage `json:"matches"`
		Bob     struct {
			Parts []struct {
				Part string `json:"part"`
			} `json:"parts"`
		} `json:"bob"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Matches) != 1 {
		t.Fatalf("matches=%d", len(payload.Matches))
	}
	if _, legacy := payload.Matches[0]["entry"]; legacy {
		t.Fatalf("v2 response contains legacy entry field: %s", recorder.Body.String())
	}
	var records []struct {
		RecordOrdinal int `json:"recordOrdinal"`
	}
	if err := json.Unmarshal(payload.Matches[0]["records"], &records); err != nil || len(records) != 2 ||
		records[0].RecordOrdinal != 1 || records[1].RecordOrdinal != 2 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	if len(payload.Bob.Parts) != 2 || payload.Bob.Parts[0].Part != "¹ noun" || payload.Bob.Parts[1].Part != "² verb" {
		t.Fatalf("Bob parts=%+v", payload.Bob.Parts)
	}
}

// TestCrossOriginRequestsAreRefused covers the drive-by case: a web page the
// user happens to have open must not be able to read their dictionary library.
func TestCrossOriginRequestsAreRefused(t *testing.T) {
	handler := newTestServer(t)
	for _, origin := range []string{
		"https://evil.example",
		"http://evil.example:8080",
		// A prefix check would wave this through: the host is evil.example.
		"http://127.0.0.1.evil.example",
		"http://localhost.evil.example",
		"http://127.0.0.1@evil.example",
		"file:///etc/passwd",
	} {
		recorder := httptest.NewRecorder()
		request := newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(`{"query":"hello"}`))
		request.Header.Set("Origin", origin)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("Origin %q got %d, want 403", origin, recorder.Code)
		}
	}
}

func TestLoopbackOriginsAreAccepted(t *testing.T) {
	handler := newTestServer(t)
	for _, origin := range []string{"http://127.0.0.1:15321", "http://localhost:3000", "null"} {
		recorder := httptest.NewRecorder()
		request := newRequest(http.MethodGet, "/v2/status", nil)
		request.Header.Set("Origin", origin)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("Origin %q got %d, want 200", origin, recorder.Code)
		}
	}
}

func TestNonLoopbackClientsAreRefused(t *testing.T) {
	handler := newTestServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v2/status", nil)
	request.RemoteAddr = "192.168.1.50:44444"
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", recorder.Code)
	}
}

// TestResourceTokensCannotBeForged is the path-traversal guard. Resolution goes
// through a per-dictionary index, so these are rejected as bad tokens rather
// than being interpreted as paths at all.
func TestResourceTokensCannotBeForged(t *testing.T) {
	handler := newTestServer(t)
	for _, token := range []string{
		"..%2f..%2f..%2fetc%2fpasswd",
		"Li4vLi4vZXRjL3Bhc3N3ZA",
		"abc",
		strings.Repeat("A", 512),
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodGet, "/v2/resource/"+token, nil))
		if recorder.Code == http.StatusOK {
			t.Errorf("token %q was served with 200", token)
		}
	}
}

func TestOversizedRequestBodyIsRejected(t *testing.T) {
	handler := newTestServer(t)
	huge := bytes.NewReader([]byte(`{"query":"` + strings.Repeat("a", 128<<10) + `"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", huge))
	if recorder.Code == http.StatusOK {
		t.Errorf("an oversized body was accepted (status %d)", recorder.Code)
	}
}

func TestRescanTakesNoPathArgument(t *testing.T) {
	handler := newTestServer(t)
	// Even if a caller supplies a path, the endpoint only ever walks the
	// configured directory.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/rescan", strings.NewReader(`{"path":"/etc"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["dictionaryCount"].(float64) != 0 {
		t.Errorf("rescan found dictionaries outside the configured directory: %v", payload)
	}
}

func TestUnknownRoutesAreNotFound(t *testing.T) {
	handler := newTestServer(t)
	for _, path := range []string{"/", "/v1", "/v1/status", "/v2/shell", "/v2/fetch"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, recorder.Code)
		}
	}
}
