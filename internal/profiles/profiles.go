// Package profiles embeds the declarative dictionary profiles and selects the
// best match for a dictionary.
package profiles

import (
	_ "embed"
	"sync"

	"github.com/wakewon/bob-plugin-mdict/internal/parser"
)

//go:embed profiles.json
var profilesJSON []byte

var (
	loadOnce sync.Once
	loaded   []*parser.Profile
	loadErr  error
)

// Profile selection lives in internal/diagnose: it needs several representative
// records to vote, not a single sample, and the same sampling serves the
// diagnostic commands. Keeping one implementation there avoids a second,
// weaker matcher drifting alongside it.

// All returns every built-in profile.
func All() ([]*parser.Profile, error) {
	loadOnce.Do(func() {
		loaded, loadErr = parser.LoadProfiles(profilesJSON)
	})
	return loaded, loadErr
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
