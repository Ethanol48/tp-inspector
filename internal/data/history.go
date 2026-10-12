package data

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"tp-inspector/internal/forge"
	"tp-inspector/internal/tp"
)

// HistoryEntry is one student's result in one activity of their promo.
type HistoryEntry struct {
	Activity forge.Activity    `json:"activity"`
	SD       string            `json:"sd"`   // graded submission definition short name
	Last     *forge.Submission `json:"last"` // newest submission of SD, nil without any
	Subs     int               `json:"subs"` // submissions on every definition
	Err      string            `json:"err,omitempty"`
}

// Slug parses the activity slug ("_d" variants included).
func (e HistoryEntry) Slug() (tp.Slug, bool) {
	s, err := tp.Parse(strings.TrimSuffix(e.Activity.Slug, "_d"))
	return s, err == nil
}

// Percent returns the grade of the newest graded submission, or -1.
func (e HistoryEntry) Percent() int {
	if e.Last == nil {
		return -1
	}
	return e.Last.Percent()
}

// Section returns the academic year (1, 2…) and promo of the activity, which
// tell apart a year that was done twice; ok is false for unknown slugs.
func (e HistoryEntry) Section() (year, promo int, ok bool) {
	s, ok := e.Slug()
	return s.Year, s.Promo, ok
}

// HistoryPromos are the promos whose activities are fetched for a student
// of the given promo: the previous one (a year they repeated) and the next
// one (a year they repeat after this assignment).
func HistoryPromos(promo int) []int { return []int{promo - 1, promo, promo + 1} }

// History is the result of a student in every activity of some promos.
type History struct {
	Login     string         `json:"login"`
	Promo     int            `json:"promo"`  // promo of the assignment it was opened from
	Promos    []int          `json:"promos"` // promos whose activities were fetched
	Entries   []HistoryEntry `json:"entries"`
	FetchedAt time.Time      `json:"fetchedAt"`
}

// HistoriesDir is the cache folder of the downloaded student histories.
const HistoriesDir = "students"

func historyName(login string, promo int) string {
	return path.Join(HistoriesDir, fmt.Sprintf("%s-%d.json", login, promo))
}

// CachedHistory returns the history saved by a previous download, if it
// covers the promos HistoryPromos(promo).
func (l *Loader) CachedHistory(login string, promo int) (*History, bool) {
	var h History
	c := &Cache{Root: l.Cache.Root}
	if _, ok := c.ReadJSON(historyName(login, promo), &h); !ok || h.Login != login ||
		fmt.Sprint(h.Promos) != fmt.Sprint(HistoryPromos(promo)) {
		return nil, false
	}
	return &h, true
}

// PromoActivities filters the activities of some promos with a
// parsable slug.
func PromoActivities(acts []forge.Activity, promos []int) []forge.Activity {
	var out []forge.Activity
	for _, a := range acts {
		s, err := tp.Parse(strings.TrimSuffix(a.Slug, "_d"))
		if err == nil && slices.Contains(promos, s.Promo) {
			out = append(out, a)
		}
	}
	return out
}

// LoadHistory fetches the submissions of one student in every activity of
// HistoryPromos(promo): one filtered explore request per activity, no traces.
func (l *Loader) LoadHistory(login string, promo int, progress Progress) (*History, error) {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	progress("Activities", 0, 0)
	all, err := l.Client.Activities()
	if err != nil {
		return nil, err
	}
	_ = l.Cache.WriteJSON("activities.json", all)
	promos := HistoryPromos(promo)
	acts := PromoActivities(all, promos)
	h := &History{Login: login, Promo: promo, Promos: promos, FetchedAt: time.Now(), Entries: make([]HistoryEntry, len(acts))}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
		sem  = make(chan struct{}, 6)
	)
	progress("Submissions", 0, len(acts))
	for i, a := range acts {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, a forge.Activity) {
			defer func() { <-sem; wg.Done() }()
			e := l.historyEntry(a, login)
			mu.Lock()
			defer mu.Unlock()
			h.Entries[i] = e
			done++
			progress("Submissions", done, len(acts))
		}(i, a)
	}
	wg.Wait()
	sort.Slice(h.Entries, func(i, j int) bool { return h.Entries[i].Activity.Slug < h.Entries[j].Activity.Slug })
	_ = (&Cache{Root: l.Cache.Root}).WriteJSON(historyName(login, promo), h)
	return h, nil
}

func (l *Loader) historyEntry(a forge.Activity, login string) HistoryEntry {
	e := HistoryEntry{Activity: a}
	group := a.TenantSlug + "-" + a.Slug + "-" + login
	var subs []forge.Submission
	for page := 0; ; page++ {
		r, pages, err := l.Client.GroupSubmissionsPage(a.URI, group, page)
		if err != nil {
			e.Err = err.Error()
			return e
		}
		subs = append(subs, r...)
		if page+1 >= pages {
			break
		}
	}
	e.Subs = len(subs)
	e.SD = gradedSD(subs)
	for i := range subs {
		s := &subs[i]
		if SDName(s.SubmissionDefinitionURI) == e.SD && (e.Last == nil || s.ReceivedAt.After(e.Last.ReceivedAt)) {
			e.Last = s
		}
	}
	return e
}

// gradedSD picks the definition that holds the grade among those of subs.
func gradedSD(subs []forge.Submission) string {
	var names []string
	for _, s := range subs {
		if !strings.Contains(s.SubmissionDefinitionURI, "/Assistants-") {
			names = append(names, SDName(s.SubmissionDefinitionURI))
		}
	}
	return GradedSDName(names)
}

// GradedSDName picks the definition that holds the grade: "submit" for TPs,
// "submission" for exams, otherwise anything but archi/dummy ones.
func GradedSDName(names []string) string {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	for _, pref := range []string{"submit", "submission"} {
		if set[pref] {
			return pref
		}
	}
	var rest []string
	for n := range set {
		if !strings.Contains(n, "archi") && !strings.Contains(n, "dummy") {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	if len(rest) > 0 {
		return rest[0]
	}
	return ""
}
