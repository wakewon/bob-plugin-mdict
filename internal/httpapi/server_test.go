package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/httpapi"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/version"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{DictionaryDir: t.TempDir(), CacheDir: t.TempDir(), Port: 15321}
	svc, err := service.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Rescan(); err != nil {
		t.Fatal(err)
	}
	return httpapi.New(svc, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))).Handler()
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
	handler.ServeHTTP(recorder, newRequest(http.MethodGet, "/v1/status", nil))

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
	request := newRequest(http.MethodPost, "/v1/lookup", strings.NewReader(`{"query":"hello"}`))
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
			handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v1/lookup", strings.NewReader(tc.body)))
			if recorder.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", recorder.Code)
			}
		})
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
		request := newRequest(http.MethodPost, "/v1/lookup", strings.NewReader(`{"query":"hello"}`))
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
		request := newRequest(http.MethodGet, "/v1/status", nil)
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
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
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
		handler.ServeHTTP(recorder, newRequest(http.MethodGet, "/v1/resource/"+token, nil))
		if recorder.Code == http.StatusOK {
			t.Errorf("token %q was served with 200", token)
		}
	}
}

func TestOversizedRequestBodyIsRejected(t *testing.T) {
	handler := newTestServer(t)
	huge := bytes.NewReader([]byte(`{"query":"` + strings.Repeat("a", 128<<10) + `"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v1/lookup", huge))
	if recorder.Code == http.StatusOK {
		t.Errorf("an oversized body was accepted (status %d)", recorder.Code)
	}
}

func TestRescanTakesNoPathArgument(t *testing.T) {
	handler := newTestServer(t)
	// Even if a caller supplies a path, the endpoint only ever walks the
	// configured directory.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v1/rescan", strings.NewReader(`{"path":"/etc"}`)))
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
	for _, path := range []string{"/", "/v1", "/v2/status", "/v1/shell", "/v1/fetch"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, recorder.Code)
		}
	}
}
