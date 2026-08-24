package mdict

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry owns every discovered dictionary and the order they are queried in.
type Registry struct {
	mu    sync.RWMutex
	root  string
	dicts []*Dictionary
	byID  map[string]*Dictionary
}

// NewRegistry creates an empty registry rooted at a dictionary directory.
func NewRegistry(root string) *Registry {
	return &Registry{root: root, byID: make(map[string]*Dictionary)}
}

// Root returns the dictionary directory being watched.
func (r *Registry) Root() string { return r.root }

// volumeSuffixRe matches the ".1", ".2" volume marker in "Dict.1.mdd".
var volumeSuffixRe = regexp.MustCompile(`\.(\d+)$`)

// splitMDDStem separates "Dict.1.mdd" into base "Dict" and volume 1.
// A plain "Dict.mdd" yields volume 0, which sorts first.
func splitMDDStem(filename string) (string, int) {
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	if match := volumeSuffixRe.FindStringSubmatch(stem); match != nil {
		volume, err := strconv.Atoi(match[1])
		if err == nil {
			return strings.TrimSuffix(stem, match[0]), volume
		}
	}
	return stem, 0
}

// Scan walks the dictionary root recursively and rebuilds the registry.
//
// Discovery is per-directory: every .mdx becomes a dictionary, and the .mdd
// files beside it whose stem matches (allowing a .N volume suffix) become its
// resource chain. Users therefore only ever copy a folder into place.
func (r *Registry) Scan() error {
	root := r.root
	if root == "" {
		return nil
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			r.replace(nil)
			return nil
		}
		return err
	}

	// Group files by containing directory in one walk.
	type dirFiles struct {
		mdx []string
		mdd []string
	}
	dirs := make(map[string]*dirFiles)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree must not abort discovery of the rest.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), "._") {
			// macOS AppleDouble sidecars are not dictionaries.
			return nil
		}
		dir := filepath.Dir(path)
		bucket := dirs[dir]
		if bucket == nil {
			bucket = &dirFiles{}
			dirs[dir] = bucket
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".mdx":
			bucket.mdx = append(bucket.mdx, path)
		case ".mdd":
			bucket.mdd = append(bucket.mdd, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	var discovered []*Dictionary
	for dir, bucket := range dirs {
		sort.Strings(bucket.mdx)
		for _, mdxPath := range bucket.mdx {
			mdxStem := strings.TrimSuffix(filepath.Base(mdxPath), filepath.Ext(mdxPath))

			type volume struct {
				path  string
				index int
			}
			var volumes []volume
			for _, mddPath := range bucket.mdd {
				stem, index := splitMDDStem(filepath.Base(mddPath))
				if !strings.EqualFold(stem, mdxStem) {
					continue
				}
				volumes = append(volumes, volume{path: mddPath, index: index})
			}
			sort.Slice(volumes, func(i, j int) bool {
				if volumes[i].index != volumes[j].index {
					return volumes[i].index < volumes[j].index
				}
				return volumes[i].path < volumes[j].path
			})
			mddPaths := make([]string, 0, len(volumes))
			for _, v := range volumes {
				mddPaths = append(mddPaths, v.path)
			}

			// When only one dictionary lives in a folder, the folder name is
			// usually a better label than the raw MDX filename.
			dirName := filepath.Base(dir)
			if dir == root {
				dirName = mdxStem
			}

			discovered = append(discovered, &Dictionary{
				info: Info{
					ID:      stableID(mdxPath),
					Title:   mdxStem,
					Profile: "generic",
					Health:  HealthOK,
				},
				mdxPath:  mdxPath,
				mddPaths: mddPaths,
				dirName:  dirName,
			})
		}
	}

	sort.Slice(discovered, func(i, j int) bool {
		return strings.ToLower(discovered[i].mdxPath) < strings.ToLower(discovered[j].mdxPath)
	})
	r.replace(discovered)
	return nil
}

func (r *Registry) replace(dicts []*Dictionary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, old := range r.dicts {
		old.Close()
	}
	r.dicts = dicts
	r.byID = make(map[string]*Dictionary, len(dicts))
	for _, dict := range dicts {
		r.byID[dict.ID()] = dict
	}
}

// LoadAll eagerly builds every index. Called at startup so the first user
// lookup is warm.
func (r *Registry) LoadAll() {
	for _, dict := range r.All() {
		_ = dict.Load()
	}
}

// All returns every dictionary in query order.
func (r *Registry) All() []*Dictionary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]*Dictionary(nil), r.dicts...)
}

// ByID returns one dictionary.
func (r *Registry) ByID(id string) (*Dictionary, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	dict, ok := r.byID[id]
	return dict, ok
}

// Select resolves a caller-supplied ID list into dictionaries, preserving the
// caller's order. An empty list means "all, in registry order".
func (r *Registry) Select(ids []string) []*Dictionary {
	if len(ids) == 0 {
		return r.All()
	}
	var out []*Dictionary
	for _, id := range ids {
		if dict, ok := r.ByID(strings.TrimSpace(id)); ok {
			out = append(out, dict)
		}
	}
	return out
}

// Counts returns the total and healthy dictionary counts.
func (r *Registry) Counts() (total int, healthy int) {
	for _, dict := range r.All() {
		total++
		if dict.Info().Health == HealthOK {
			healthy++
		}
	}
	return total, healthy
}
