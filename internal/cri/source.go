package cri

import (
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"tp-inspector/internal/data"
)

const (
	catalogTTL = 7 * 24 * time.Hour // classes barely change during a semester
	membersTTL = 24 * time.Hour
)

// Catalog is every class, parsed and sorted, with the campus names.
type Catalog struct {
	Campuses []Campus `json:"campuses"`
	Classes  []Class  `json:"classes"`
}

// CampusName returns the name of a campus slug ("Other" when unknown).
func (c *Catalog) CampusName(slug string) string {
	for _, cp := range c.Campuses {
		if cp.Slug == slug {
			return cp.Name
		}
	}
	if slug == "" {
		return "Other"
	}
	return slug
}

// Source serves classes and members from the cache, asking the API only when
// the cached copy is missing or old. Safe for concurrent use (TUI and web).
type Source struct {
	Client *Client
	Cache  *data.Cache // rooted at <cache root>/cri

	mu sync.Mutex
}

// NewSource caches under root/data.CRIDir; refresh ignores cached copies.
func NewSource(client *Client, root string, refresh bool) *Source {
	return &Source{Client: client, Cache: &data.Cache{Root: filepath.Join(root, data.CRIDir), Refresh: refresh}}
}

func (s *Source) fresh(name string, v any, ttl time.Duration) bool {
	t, ok := s.Cache.ReadJSON(name, v)
	return ok && time.Since(t) < ttl
}

// Catalog returns every class of every campus.
func (s *Source) Catalog() (*Catalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cat Catalog
	if s.fresh("classes.json", &cat, catalogTTL) && len(cat.Classes) > 0 {
		return &cat, nil
	}
	campuses, err := s.Client.Campuses()
	if err != nil {
		return nil, err
	}
	groups, err := s.Client.Groups("class")
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, cp := range campuses {
		known[cp.Slug] = true
	}
	cat = Catalog{Campuses: campuses, Classes: make([]Class, len(groups))}
	for i, g := range groups {
		cat.Classes[i] = ParseClass(g, known)
	}
	SortClasses(cat.Classes)
	_ = s.Cache.WriteJSON("classes.json", cat)
	return &cat, nil
}

// Members returns the logins of a class.
func (s *Source) Members(slug string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := "members/" + slug + ".json"
	var out []string
	if s.fresh(name, &out, membersTTL) {
		return out, nil
	}
	out, err := s.Client.Members(slug)
	if err != nil {
		return nil, err
	}
	_ = s.Cache.WriteJSON(name, out)
	return out, nil
}

// MembersOf returns the logins of several classes, without duplicates.
func (s *Source) MembersOf(slugs []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, slug := range slugs {
		ms, err := s.Members(slug)
		if err != nil {
			return nil, err
		}
		for _, l := range ms {
			if !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	return out, nil
}

// Filter keeps the classes that can take an activity: those of its semester.
type Filter struct {
	Semesters map[int]bool // empty: every class
}

// ActivityFilter derives the semester from an activity slug's year and
// bimester: 4 bimesters a year (year 2 = bimesters 5–8), 2 per semester, so
// prog-206 is S3. When the bimester does not fall in the year, both
// semesters of the year are kept.
func ActivityFilter(year, bimester int) Filter {
	if year <= 0 {
		return Filter{}
	}
	lo, hi := 2*year-1, 2*year
	if sem := (bimester + 1) / 2; sem >= lo && sem <= hi {
		return Filter{Semesters: map[int]bool{sem: true}}
	}
	return Filter{Semesters: map[int]bool{lo: true, hi: true}}
}

func (f Filter) Match(c Class) bool {
	return len(f.Semesters) == 0 || f.Semesters[c.Semester]
}

// Label is "S3", "S3–S4" or "" for every class.
func (f Filter) Label() string {
	lo, hi := 0, 0
	for s := range f.Semesters {
		if lo == 0 || s < lo {
			lo = s
		}
		hi = max(hi, s)
	}
	switch {
	case lo == 0:
		return ""
	case lo == hi:
		return "S" + strconv.Itoa(lo)
	}
	return "S" + strconv.Itoa(lo) + "–S" + strconv.Itoa(hi)
}
