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
		strings.NewReader(`{"query":"flimber","format":"bob","limit":1,"multiRecordMode":"combined"}`)))
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
	var lookupKey string
	if err := json.Unmarshal(payload.Matches[0]["lookupKey"], &lookupKey); err != nil || lookupKey != "flimber" {
		t.Fatalf("lookupKey=%q err=%v response=%s", lookupKey, err, recorder.Body.String())
	}
	var records []struct {
		RecordOrdinal int `json:"recordOrdinal"`
	}
	if err := json.Unmarshal(payload.Matches[0]["records"], &records); err != nil || len(records) != 2 ||
		records[0].RecordOrdinal != 1 || records[1].RecordOrdinal != 2 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	if len(payload.Bob.Parts) != 2 || payload.Bob.Parts[0].Part != "¹ n." || payload.Bob.Parts[1].Part != "² v." {
		t.Fatalf("Bob parts=%+v", payload.Bob.Parts)
	}
}

func TestV2LookupMarkdownIsAdditiveUserOutput(t *testing.T) {
	root := t.TempDir()
	markup := `<article><h1>flimber</h1><div class="sense"><span class="pos">noun</span>` +
		`<span class="grammar">[synthetic grammar]</span>` +
		`<span class="definition">synthetic definition</span>` +
		`<span class="example">first synthetic example</span><span class="example">second synthetic example</span></div></article>`
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), []testmdx.Entry{{Key: "flimber", HTML: markup}}); err != nil {
		t.Fatal(err)
	}
	handler := newTestServerForDir(t, root)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(
		`{"query":"flimber","format":"markdown","includeExamples":false,"includeExtras":false,"maxExamples":1}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Matches  []json.RawMessage `json:"matches"`
		Markdown string            `json:"markdown"`
		Bob      json.RawMessage   `json:"bob"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Matches) != 1 || payload.Markdown == "" || len(payload.Bob) != 0 {
		t.Fatalf("markdown response lost IR or added Bob: %s", recorder.Body.String())
	}
	if !strings.Contains(payload.Markdown, "synthetic grammar") {
		t.Errorf("omitted includeGrammar should preserve default true, but grammar is missing:\n%s", payload.Markdown)
	}
	for _, forbidden := range []string{"synthetic example", "generic:", "confidence", "validation"} {
		if strings.Contains(payload.Markdown, forbidden) {
			t.Errorf("user Markdown contains %q:\n%s", forbidden, payload.Markdown)
		}
	}
}

func TestV2LookupPlainAndAutomaticBobFallbackAreAdditive(t *testing.T) {
	root := t.TempDir()
	markup := `<article><p>first synthetic paragraph</p><p>second synthetic paragraph</p></article>`
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), []testmdx.Entry{{Key: "flimber", HTML: markup}}); err != nil {
		t.Fatal(err)
	}
	handler := newTestServerForDir(t, root)

	lookup := func(format string) struct {
		Matches         []json.RawMessage `json:"matches"`
		Plain           string            `json:"plain"`
		Bob             json.RawMessage   `json:"bob"`
		EffectiveFormat string            `json:"effectiveFormat"`
	} {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(
			`{"query":"flimber","format":"`+format+`","limit":1}`)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", format, recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Matches         []json.RawMessage `json:"matches"`
			Plain           string            `json:"plain"`
			Bob             json.RawMessage   `json:"bob"`
			EffectiveFormat string            `json:"effectiveFormat"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	explicit := lookup("plain")
	if explicit.EffectiveFormat != "plain" || explicit.Plain == "" || len(explicit.Matches) != 1 || len(explicit.Bob) != 0 {
		t.Fatalf("explicit Plain response = %+v", explicit)
	}
	if !strings.Contains(explicit.Plain, "first synthetic paragraph\n\nsecond synthetic paragraph") {
		t.Fatalf("Plain did not preserve paragraph blocks:\n%s", explicit.Plain)
	}

	fallback := lookup("bob")
	if fallback.EffectiveFormat != "plain" || fallback.Plain != explicit.Plain || len(fallback.Bob) != 0 {
		t.Fatalf("Bob fallback response = %+v", fallback)
	}
}

// multiRecordMode means the same thing in both presentations. What differs is
// only how each one can draw it: Bob has clickable related words, Markdown has
// a thematic break and copyable query text.
func TestV2LookupMarkdownHonoursMultiRecordMode(t *testing.T) {
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

	markdownFor := func(body string) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(body)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Markdown string `json:"markdown"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Markdown
	}

	combined := markdownFor(`{"query":"flimber","format":"markdown","multiRecordMode":"combined"}`)
	if !strings.Contains(combined, "synthetic noun definition") || !strings.Contains(combined, "synthetic verb definition") {
		t.Errorf("combined Markdown dropped a record:\n%s", combined)
	}
	if got := strings.Count(combined, "\n---\n"); got != 1 {
		t.Errorf("two records need exactly one divider, got %d:\n%s", got, combined)
	}

	separate := markdownFor(`{"query":"flimber","format":"markdown","multiRecordMode":"separate"}`)
	if !strings.Contains(separate, "synthetic noun definition") || strings.Contains(separate, "synthetic verb definition") {
		t.Errorf("separate Markdown should show record 1 only:\n%s", separate)
	}
	if !strings.Contains(separate, "## Other entries") || !strings.Contains(separate, "- `flimber\u00b2`") {
		t.Errorf("separate Markdown must offer the sibling record as copyable text:\n%s", separate)
	}
	if strings.Contains(separate, "](") {
		t.Errorf("Bob has no Markdown lookup contract, so nothing may become a link:\n%s", separate)
	}

	selected := markdownFor(`{"query":"flimber","format":"markdown","multiRecordMode":"separate","recordOrdinal":2}`)
	if !strings.Contains(selected, "synthetic verb definition") || strings.Contains(selected, "synthetic noun definition") {
		t.Errorf("record selector 2 should show record 2 only:\n%s", selected)
	}
	if !strings.Contains(selected, "- `flimber\u00b9`") {
		t.Errorf("the selected record should point back at its sibling:\n%s", selected)
	}

	// An out-of-range selector is a 404 in Markdown exactly as in the Bob card.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup",
		strings.NewReader(`{"query":"flimber","format":"markdown","recordOrdinal":9}`)))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "recordNotFound") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// markdownSource "html" converts the record's own HTML with the stylesheet
// beside the MDX, and keeps every record boundary and selector the structured
// view has, so the two views name the same records.
func TestV2LookupMarkdownFromRecordHTML(t *testing.T) {
	root := t.TempDir()
	markup := func(pos, definition string) string {
		return `<link rel="stylesheet" href="synthetic.css"><link rel="stylesheet" href="absent.css">` +
			`<span class="hw">flimber</span><span class="sense"><span class="pos">` + pos +
			`</span> <span class="definition">` + definition + `</span><span class="zh">隐藏</span></span>` +
			`<span class="sense">see <a href="entry://wexal">wexal</a></span>`
	}
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), []testmdx.Entry{
		{Key: "flimber", HTML: markup("noun", "synthetic noun definition")},
		{Key: "flimber", HTML: markup("verb", "synthetic verb definition")},
		{Key: "flimbers", HTML: "@@@LINK=flimber"},
	}); err != nil {
		t.Fatal(err)
	}
	css := `.hw{font-weight:bold} .sense{display:block} .pos{font-style:italic} .zh{display:none}`
	if err := os.WriteFile(filepath.Join(root, "synthetic.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := newTestServerForDir(t, root)

	markdownFor := func(body string) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(body)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Markdown        string `json:"markdown"`
			EffectiveFormat string `json:"effectiveFormat"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.EffectiveFormat != "markdown" {
			t.Fatalf("effectiveFormat = %q", payload.EffectiveFormat)
		}
		return payload.Markdown
	}

	separate := markdownFor(`{"query":"flimber","format":"markdown","markdownSource":"html"}`)
	want := "**flimber**\n\n*noun* synthetic noun definition\n\nsee wexal\n\n## Other entries\n\n- `flimber²`\n"
	if separate != want {
		t.Errorf("separate:\n%s\nwant:\n%s", separate, want)
	}

	combined := markdownFor(`{"query":"flimbers","format":"markdown","markdownSource":"HTML","multiRecordMode":"combined"}`)
	if !strings.Contains(combined, "## Record 1 of 2") || !strings.Contains(combined, "\n---\n\n## Record 2 of 2") ||
		!strings.Contains(combined, "synthetic verb definition") {
		t.Errorf("combined Markdown through a redirect lost a record boundary:\n%s", combined)
	}

	selected := markdownFor(`{"query":"flimber","format":"markdown","markdownSource":"html","recordOrdinal":2}`)
	if !strings.Contains(selected, "synthetic verb definition") || !strings.Contains(selected, "- `flimber¹`") {
		t.Errorf("record selector 2:\n%s", selected)
	}

	structured := markdownFor(`{"query":"flimber","format":"markdown","markdownSource":"entry"}`)
	if !strings.HasPrefix(structured, "# flimber") {
		t.Errorf("markdownSource entry must stay the structured view:\n%s", structured)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup",
		strings.NewReader(`{"query":"flimber","format":"markdown","markdownSource":"pdf"}`)))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "entry or html") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodGet, "/v2/dictionaries", nil))
	var list httpapi.DictionariesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Dictionaries) != 1 || strings.Join(list.Dictionaries[0].MissingStylesheets, ",") != "absent.css" {
		t.Errorf("missing stylesheets = %+v", list.Dictionaries)
	}
}

// Prepared audio is reachable only by POST from a non-browser caller — the
// link helper — and only for a token the service minted.
func TestV2AudioIsGuarded(t *testing.T) {
	handler := newTestServer(t)
	for _, tc := range []struct {
		method, origin string
		want           int
	}{
		{http.MethodPost, "", http.StatusBadRequest},
		{http.MethodGet, "", http.StatusMethodNotAllowed},
		{http.MethodPost, "https://example.invalid", http.StatusForbidden},
	} {
		request := newRequest(tc.method, "/v2/audio/not-a-real-token", nil)
		if tc.origin != "" {
			request.Header.Set("Origin", tc.origin)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != tc.want {
			t.Errorf("%s origin=%q: status %d, want %d", tc.method, tc.origin, recorder.Code, tc.want)
		}
	}
}

// A page reached through a dictionary link leads back to where the reader
// came from, and every link on it says which page it is on.
func TestV2NavigationOffersTheWayBack(t *testing.T) {
	root := t.TempDir()
	page := func(word, other string) string {
		return `<p>` + word + ` see <a href="entry://` + other + `">` + other + `</a></p>`
	}
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), []testmdx.Entry{
		{Key: "wexal", HTML: page("wexal", "flimber")},
		{Key: "flimber", HTML: page("flimber", "wexal")},
		{Key: "zorble", HTML: page("zorble", "wexal")},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DictionaryDir: root, CacheDir: t.TempDir(), Port: 15321}
	svc, err := service.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Rescan(); err != nil {
		t.Fatal(err)
	}
	svc.EnableLookupLinks()
	handler := httpapi.New(svc, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()

	markdownFor := func(word string) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup",
			strings.NewReader(`{"query":"`+word+`","format":"markdown","markdownSource":"html"}`)))
		var payload struct {
			Markdown string `json:"markdown"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Markdown
	}
	navigate := func(form string) {
		t.Helper()
		request := newRequest(http.MethodPost, "/v2/navigation", strings.NewReader(form))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("navigation %q: status %d %s", form, recorder.Code, recorder.Body.String())
		}
	}

	if got := markdownFor("wexal"); got != "wexal see [flimber](bobmdict://lookup?text=flimber&from=wexal&port=15321)\n" {
		t.Fatalf("a page looked up by hand:\n%s", got)
	}
	navigate("to=flimber&from=wexal")
	want := "flimber see [wexal](bobmdict://lookup?text=wexal&from=flimber&port=15321)\n\n---\n\n" +
		"[← wexal](bobmdict://lookup?text=wexal&back=1&port=15321)\n"
	if got := markdownFor("flimber"); got != want {
		t.Fatalf("a page reached by a link:\n%s\nwant:\n%s", got, want)
	}
	if got := markdownFor("zorble"); strings.Contains(got, "←") {
		t.Fatalf("a page off the path offers a way back:\n%s", got)
	}
	navigate("to=wexal&back=1")
	if got := markdownFor("wexal"); strings.Contains(got, "←") {
		t.Fatalf("back at the start of the path:\n%s", got)
	}

	recorder := httptest.NewRecorder()
	request := newRequest(http.MethodPost, "/v2/navigation", strings.NewReader("to=flimber"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("a step with no origin: status %d", recorder.Code)
	}
}

func TestV2LookupRejectsUnknownFormat(t *testing.T) {
	handler := newTestServer(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(`{"query":"hello","format":"html"}`)))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "ir, bob, plain, or markdown") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestV2LookupExposesInputAndActualLookupKeySeparately(t *testing.T) {
	root := t.TempDir()
	markup := `<article><h1>china1</h1><div class="sense"><span class="pos">noun</span>` +
		`<span class="definition">synthetic lowercase definition</span></div></article>`
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), []testmdx.Entry{{Key: "china", HTML: markup}}); err != nil {
		t.Fatal(err)
	}
	handler := newTestServerForDir(t, root)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup",
		strings.NewReader(`{"query":"CHINA","format":"bob","limit":1}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Query   string `json:"query"`
		Matches []struct {
			LookupKey string `json:"lookupKey"`
			Headword  string `json:"headword"`
			Records   []struct {
				Entry struct {
					Source struct {
						MatchedKey string `json:"matchedKey"`
					} `json:"source"`
				} `json:"entry"`
			} `json:"records"`
		} `json:"matches"`
		Bob struct {
			Word string `json:"word"`
		} `json:"bob"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Query != "CHINA" || len(payload.Matches) != 1 || payload.Matches[0].LookupKey != "china" ||
		payload.Matches[0].Headword != "china1" || len(payload.Matches[0].Records) != 1 ||
		payload.Matches[0].Records[0].Entry.Source.MatchedKey != "china" ||
		payload.Bob.Word != "china" {
		t.Fatalf("v2 provenance response = %+v", payload)
	}
}

func TestV2LookupSeparatesAndSelectsVisibleRecordOrdinals(t *testing.T) {
	root := t.TempDir()
	markup := func(pos, definition string) string {
		return `<article><h1>foo</h1><div class="sense"><span class="pos">` + pos +
			`</span><span class="definition">` + definition + `</span></div></article>`
	}
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), []testmdx.Entry{
		{Key: "foo", HTML: markup("noun", "first definition")},
		{Key: "foo", HTML: `<div class="technical"></div>`},
		{Key: "foo", HTML: markup("verb", "second definition")},
	}); err != nil {
		t.Fatal(err)
	}
	handler := newTestServerForDir(t, root)

	lookup := func(body string) (int, map[string]any) {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(body)))
		var payload map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s: %v", recorder.Body.String(), err)
		}
		return recorder.Code, payload
	}

	code, payload := lookup(`{"query":"foo","format":"bob","limit":1,"multiRecordMode":"separate"}`)
	if code != http.StatusOK {
		t.Fatalf("default separate status=%d payload=%+v", code, payload)
	}
	bob := payload["bob"].(map[string]any)
	if bob["word"] != "foo" || len(bob["parts"].([]any)) != 1 {
		t.Fatalf("default separate bob=%+v", bob)
	}
	groups := bob["relatedWordParts"].([]any)
	other := groups[len(groups)-1].(map[string]any)
	words := other["words"].([]any)
	if other["part"] != "Other entries" || len(words) != 1 || words[0].(map[string]any)["word"] != "foo²" {
		t.Fatalf("default separate navigation=%+v", groups)
	}

	code, payload = lookup(`{"query":"foo","recordOrdinal":2,"multiRecordMode":"separate","format":"bob","limit":1}`)
	if code != http.StatusOK {
		t.Fatalf("explicit second status=%d payload=%+v", code, payload)
	}
	bob = payload["bob"].(map[string]any)
	parts := bob["parts"].([]any)
	if bob["word"] != "foo²" || len(parts) != 1 || parts[0].(map[string]any)["part"] != "v." {
		t.Fatalf("explicit second bob=%+v", bob)
	}
	groups = bob["relatedWordParts"].([]any)
	words = groups[len(groups)-1].(map[string]any)["words"].([]any)
	if len(words) != 1 || words[0].(map[string]any)["word"] != "foo¹" {
		t.Fatalf("explicit second siblings=%+v", groups)
	}

	code, payload = lookup(`{"query":"foo","recordOrdinal":3,"multiRecordMode":"separate","format":"bob","limit":1}`)
	if code != http.StatusNotFound || payload["error"] != "recordNotFound" || !strings.Contains(payload["message"].(string), "只有 2 个") {
		t.Fatalf("out-of-range status=%d payload=%+v", code, payload)
	}
}

func TestV2LookupValidatesMultiRecordPresentationOptions(t *testing.T) {
	handler := newTestServer(t)
	for _, body := range []string{
		`{"query":"foo","recordOrdinal":-1}`,
		`{"query":"foo","multiRecordMode":"sideways"}`,
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/v2/lookup", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "badRequest") {
			t.Fatalf("body=%s status=%d response=%s", body, recorder.Code, recorder.Body.String())
		}
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
