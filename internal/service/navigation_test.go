package service

import (
	"testing"
	"time"
)

func TestNavigationLeadsBackAlongThePath(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nav := newNavigation()
	nav.now = func() time.Time { return clock }

	if got := nav.previous("hello"); got != "" {
		t.Fatalf("an unvisited page leads back to %q", got)
	}
	nav.visit("hello", "greeting")
	nav.visit("greeting", "salutation")
	if got := nav.previous("Salutation"); got != "greeting" {
		t.Fatalf("previous(salutation) = %q", got)
	}
	nav.back("greeting")
	if got := nav.previous("greeting"); got != "hello" {
		t.Fatalf("after one step back, previous(greeting) = %q", got)
	}
	// A page looked up by hand is not on the path.
	if got := nav.previous("unrelated"); got != "" {
		t.Fatalf("previous(unrelated) = %q", got)
	}
	// A link on a page that is not where the path ends starts a new path.
	nav.visit("unrelated", "other")
	if got := nav.previous("other"); got != "unrelated" {
		t.Fatalf("new path: previous(other) = %q", got)
	}
	nav.back("unrelated")
	if got := nav.previous("unrelated"); got != "" {
		t.Fatalf("back at the start of a path, previous = %q", got)
	}
	// Record selectors are pages of their own.
	nav.visit("wound", "wound²")
	if got := nav.previous("wound²"); got != "wound" {
		t.Fatalf("previous(wound²) = %q", got)
	}
	// Looked up again later, by hand, the page was not reached by the link.
	clock = clock.Add(arrivalWindow + time.Second)
	if got := nav.previous("wound²"); got != "" {
		t.Fatalf("a later lookup of the same word leads back to %q", got)
	}
	// A link on that page still continues the path.
	nav.visit("wound²", "injure")
	if got := nav.previous("injure"); got != "wound²" {
		t.Fatalf("previous(injure) = %q", got)
	}
	nav.back("wound²")
	if got := nav.previous("wound²"); got != "wound" {
		t.Fatalf("back along a path resumed after a pause: previous = %q", got)
	}
	clock = clock.Add(navigationExpiry + time.Minute)
	nav.visit("elsewhere", "x")
	nav.back("elsewhere")
	if got := nav.previous("elsewhere"); got != "" {
		t.Fatalf("an abandoned path still leads back to %q", got)
	}
}

func TestNavigationPathIsBounded(t *testing.T) {
	nav := newNavigation()
	previous := "w0"
	for i := 1; i <= navigationDepth+10; i++ {
		next := "w" + string(rune('a'+i%26)) + string(rune('0'+i%10)) + string(rune('A'+i/26))
		nav.visit(previous, next)
		previous = next
	}
	if len(nav.path) != navigationDepth {
		t.Fatalf("path length %d, want %d", len(nav.path), navigationDepth)
	}
}
