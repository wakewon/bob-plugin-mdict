package service

import (
	"strings"
	"sync"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
)

// navigation remembers the path a reader has taken through dictionary links,
// so a page reached by a link can lead back.
//
// Bob hands the plugin only the text to look up, so where the reader came
// from cannot travel with the lookup itself. The link helper reports each
// step here just before it asks Bob, and the page it opens then finds itself
// at the end of the path. A word looked up any other way is not on the path
// and gets no way back; the next link click starts a new path from it.
type navigation struct {
	mu      sync.Mutex
	path    []string
	updated time.Time
	now     func() time.Time
}

const (
	// navigationDepth bounds how far back the path reaches.
	navigationDepth = 50
	// navigationExpiry forgets a path the reader has left alone.
	navigationExpiry = 30 * time.Minute
	// arrivalWindow is how soon after a step the page it opened is rendered:
	// Bob looks the word up the moment the helper asks. A lookup of the same
	// word any later was typed by the reader, not reached by the link, and
	// offers no way back — though a link on it still continues the path.
	arrivalWindow = 30 * time.Second
)

func newNavigation() *navigation { return &navigation{now: time.Now} }

func sameQuery(a, b string) bool {
	return strings.EqualFold(mdict.NormalizeExactKey(a), mdict.NormalizeExactKey(b))
}

func (n *navigation) expired() bool {
	return len(n.path) == 0 || n.now().Sub(n.updated) > navigationExpiry
}

// visit records a step from one page to another. A step that does not start
// where the path ends starts a new path.
func (n *navigation) visit(from, to string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.expired() || !sameQuery(n.path[len(n.path)-1], from) {
		n.path = []string{from}
	}
	if !sameQuery(n.path[len(n.path)-1], to) {
		n.path = append(n.path, to)
	}
	if len(n.path) > navigationDepth {
		n.path = append([]string(nil), n.path[len(n.path)-navigationDepth:]...)
	}
	n.updated = n.now()
}

// back records a step back to the page before this one.
func (n *navigation) back(to string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.expired() && len(n.path) >= 2 && sameQuery(n.path[len(n.path)-2], to) {
		n.path = n.path[:len(n.path)-1]
	} else {
		n.path = []string{to}
	}
	n.updated = n.now()
}

// previous is the page before current on the path, or "" when current is
// not where the path ends, nothing came before it, or current is not being
// opened by the step just taken.
func (n *navigation) previous(current string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.expired() || n.now().Sub(n.updated) > arrivalWindow ||
		len(n.path) < 2 || !sameQuery(n.path[len(n.path)-1], current) {
		return ""
	}
	return n.path[len(n.path)-2]
}
