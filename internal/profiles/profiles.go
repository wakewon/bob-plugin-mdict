// Package profiles embeds the declarative dictionary profiles and selects the
// best match for a dictionary.
package profiles

import (
	_ "embed"
	"sync"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/parser"
)

//go:embed profiles.json
var profilesJSON []byte

var (
	loadOnce sync.Once
	loaded   []*parser.Profile
	loadErr  error
)

// All returns every built-in profile.
func All() ([]*parser.Profile, error) {
	loadOnce.Do(func() {
		loaded, loadErr = parser.LoadProfiles(profilesJSON)
	})
	return loaded, loadErr
}

// Match picks the profile that best fits a dictionary, given its title and a
// parsed sample entry. It returns nil when no profile applies, in which case
// the generic parser handles the dictionary.
func Match(title string, sample *html.Node) *parser.Profile {
	all, err := All()
	if err != nil || sample == nil {
		return nil
	}
	var best *parser.Profile
	bestScore := 0
	for _, profile := range all {
		if score := profile.Fingerprint(title, sample); score > bestScore {
			best, bestScore = profile, score
		}
	}
	return best
}

// ByID returns a profile by identifier.
func ByID(id string) *parser.Profile {
	all, err := All()
	if err != nil {
		return nil
	}
	for _, profile := range all {
		if profile.ID == id {
			return profile
		}
	}
	return nil
}
