// Package data loads everything tpi needs about one activity.
package data

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"tp-inspector/internal/forge"
	"tp-inspector/internal/tp"
	"tp-inspector/internal/trace"
)

// Student aggregates one group's data (groups are single-member in practice).
type Student struct {
	Login  string
	Group  forge.Group
	Subs   []forge.Submission                 // newest first, all submission definitions
	Picked map[string]*forge.PickedSubmission // by submission definition short name
	Traces map[string]*trace.Trace            // by submission definition short name
}

// SubsFor returns the submissions for one definition, newest first.
func (s *Student) SubsFor(sd string) []forge.Submission {
	var out []forge.Submission
	for _, sub := range s.Subs {
		if SDName(sub.SubmissionDefinitionURI) == sd {
			out = append(out, sub)
		}
	}
	return out
}

// Last returns the picked (graded) submission for a definition, or nil.
func (s *Student) Last(sd string) *forge.Submission {
	subs := s.SubsFor(sd)
	if p := s.Picked[sd]; p != nil {
		for i := range subs {
			if subs[i].ID == p.SubmissionID {
				return &subs[i]
			}
		}
	}
	if len(subs) > 0 {
		return &subs[0]
	}
	return nil
}

// Percent returns the graded percentage for a definition, or -1.
func (s *Student) Percent(sd string) int {
	if last := s.Last(sd); last != nil && last.Percent() >= 0 {
		return last.Percent()
	}
	if t := s.Traces[sd]; t != nil && t.Percent >= 0 {
		return t.Percent
	}
	return -1
}

// Dataset is everything known about one activity.
type Dataset struct {
	Slug      tp.Slug
	Activity  forge.Activity
	SDs       []string          // student submission definition short names, Graded first
	Graded    string            // the definition that holds the grade ("submit", "submission"…), "" if none
	SDURIs    map[string]string // short name -> full URI
	Students  []*Student        // sorted by login
	ByLogin   map[string]*Student
	FetchedAt time.Time
	Warnings  []string
}

// SDName returns the short name of a submission definition URI.
func SDName(uri string) string { return path.Base(uri) }

// Progress reports loading progress; total is 0 for indeterminate steps.
type Progress func(step string, done, total int)

// Loader fetches a Dataset through the API with caching.
type Loader struct {
	Client *forge.Client
	Cache  *Cache
	Tenant string // optional; resolved from the activities list otherwise
}

func (l *Loader) Load(slug tp.Slug, progress Progress) (*Dataset, error) {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	ds := &Dataset{Slug: slug, SDURIs: map[string]string{}, ByLogin: map[string]*Student{}, FetchedAt: time.Now()}

	progress("Looking up the activity", 0, 0)
	act, err := l.resolveActivity(slug)
	if err != nil {
		return nil, err
	}
	ds.Activity = act
	actCache := &Cache{Root: l.Cache.path(act.Slug), Refresh: l.Cache.Refresh}

	progress("Submission definitions", 0, 0)
	var sdURIs []string
	if _, ok := actCache.ReadJSON("sds.json", &sdURIs); !ok {
		if sdURIs, err = l.Client.SubmissionDefinitions(act.URI); err != nil {
			return nil, err
		}
		_ = actCache.WriteJSON("sds.json", sdURIs)
	}
	for _, u := range sdURIs {
		name := SDName(u)
		if strings.Contains(u, "/Assistants-") || strings.Contains(name, "dummy") {
			continue
		}
		ds.SDs = append(ds.SDs, name)
		ds.SDURIs[name] = u
	}
	ds.Graded = GradedSDName(ds.SDs)
	sort.SliceStable(ds.SDs, func(i, j int) bool { return ds.SDs[i] == ds.Graded && ds.SDs[j] != ds.Graded })
	if len(ds.SDs) == 0 {
		return nil, fmt.Errorf("%s has no student submission definitions", act.URI)
	}

	progress("Groups", 0, 0)
	var groups []forge.Group
	if _, ok := actCache.ReadJSON("groups.json", &groups); !ok {
		if groups, err = l.Client.Groups(act.URI); err != nil {
			return nil, err
		}
		_ = actCache.WriteJSON("groups.json", groups)
	}
	prefix := act.TenantSlug + "-" + act.Slug + "-"
	for _, g := range groups {
		st := &Student{Login: strings.TrimPrefix(g.Slug, prefix), Group: g,
			Picked: map[string]*forge.PickedSubmission{}, Traces: map[string]*trace.Trace{}}
		ds.Students = append(ds.Students, st)
		ds.ByLogin[g.Slug] = st
	}

	subs, fetched, err := l.submissions(act, actCache, progress)
	if err != nil {
		return nil, err
	}
	if !fetched.IsZero() {
		ds.FetchedAt = fetched
	}
	for _, sub := range subs {
		st := ds.student(sub.GroupSlug, prefix)
		st.Subs = append(st.Subs, sub)
	}
	for _, st := range ds.Students {
		sort.Slice(st.Subs, func(i, j int) bool { return st.Subs[i].ReceivedAt.After(st.Subs[j].ReceivedAt) })
	}

	// Picked submissions carry short-lived presigned trace URLs: never cached.
	type job struct {
		st *Student
		sd string
		pk *forge.PickedSubmission
	}
	var jobs []job
	for _, sd := range ds.SDs {
		progress("Last submissions ("+sd+")", 0, 0)
		picked, err := l.Client.PickedLast(ds.SDURIs[sd])
		if err != nil {
			return nil, err
		}
		for i := range picked {
			pk := &picked[i]
			st := ds.student(pk.GroupSlug, prefix)
			st.Picked[sd] = pk
			if pk.CurrentJobID != "" {
				jobs = append(jobs, job{st, sd, pk})
			}
		}
	}

	var (
		mu   sync.Mutex
		done int
		wg   sync.WaitGroup
		sem  = make(chan struct{}, 16)
	)
	progress("Traces", 0, len(jobs))
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer func() { <-sem; wg.Done() }()
			tr, err := l.trace(actCache, j.pk)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				ds.Warnings = append(ds.Warnings, fmt.Sprintf("trace %s (%s): %v", j.st.Login, j.sd, err))
			} else {
				j.st.Traces[j.sd] = tr
			}
			done++
			progress("Traces", done, len(jobs))
		}(j)
	}
	wg.Wait()

	sort.Slice(ds.Students, func(i, j int) bool { return ds.Students[i].Login < ds.Students[j].Login })
	return ds, nil
}

// student returns the student for a group slug, creating it if the group
// list did not contain it.
func (ds *Dataset) student(groupSlug, prefix string) *Student {
	if st := ds.ByLogin[groupSlug]; st != nil {
		return st
	}
	st := &Student{Login: strings.TrimPrefix(groupSlug, prefix), Group: forge.Group{Slug: groupSlug},
		Picked: map[string]*forge.PickedSubmission{}, Traces: map[string]*trace.Trace{}}
	ds.Students = append(ds.Students, st)
	ds.ByLogin[groupSlug] = st
	return st
}

func (l *Loader) resolveActivity(slug tp.Slug) (forge.Activity, error) {
	if l.Tenant != "" {
		return forge.Activity{URI: l.Tenant + "/" + slug.Raw, Slug: slug.Raw, TenantSlug: l.Tenant, Name: slug.Raw}, nil
	}
	var acts []forge.Activity
	if _, ok := l.Cache.ReadJSON("activities.json", &acts); !ok || findActivity(acts, slug.Raw) == nil {
		var err error
		if acts, err = l.Client.Activities(); err != nil {
			return forge.Activity{}, err
		}
		_ = l.Cache.WriteJSON("activities.json", acts)
	}
	if a := findActivity(acts, slug.Raw); a != nil {
		return *a, nil
	}
	return forge.Activity{}, fmt.Errorf("you have no access to an activity %q (or it does not exist)", slug.Raw)
}

func findActivity(acts []forge.Activity, slug string) *forge.Activity {
	for i := range acts {
		if acts[i].Slug == slug {
			return &acts[i]
		}
	}
	return nil
}

func (l *Loader) submissions(act forge.Activity, c *Cache, progress Progress) ([]forge.Submission, time.Time, error) {
	var subs []forge.Submission
	if t, ok := c.ReadJSON("submissions.json", &subs); ok {
		return subs, t, nil
	}
	first, pages, err := l.Client.SubmissionsPage(act.URI, 0)
	if err != nil {
		return nil, time.Time{}, err
	}
	results := make([][]forge.Submission, pages)
	if pages > 0 {
		results[0] = first
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		done    = 1
		firstEr error
		sem     = make(chan struct{}, 6)
	)
	progress("Submission history", done, pages)
	for p := 1; p < pages; p++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer func() { <-sem; wg.Done() }()
			r, _, err := l.Client.SubmissionsPage(act.URI, p)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstEr == nil {
				firstEr = err
			}
			results[p] = r
			done++
			progress("Submission history", done, pages)
		}(p)
	}
	wg.Wait()
	if firstEr != nil {
		return nil, time.Time{}, firstEr
	}
	// Pages can shift while new submissions arrive: dedupe by id.
	seen := map[string]bool{}
	for _, r := range results {
		for _, s := range r {
			if !seen[s.ID] {
				seen[s.ID] = true
				subs = append(subs, s)
			}
		}
	}
	_ = c.WriteJSON("submissions.json", subs)
	return subs, time.Time{}, nil
}

// trace returns a parsed trace, cached on disk by job id (jobs are immutable).
func (l *Loader) trace(c *Cache, pk *forge.PickedSubmission) (*trace.Trace, error) {
	name := path.Join("traces", pk.CurrentJobID+".xml")
	b, ok := c.ReadFile(name)
	if !ok {
		if pk.TracePresignedURL == "" {
			return nil, fmt.Errorf("no trace")
		}
		var err error
		if b, err = l.Client.Download(pk.TracePresignedURL); err != nil {
			return nil, err
		}
		_ = c.WriteFile(name, b)
	}
	return trace.Parse(b)
}

// JobTrace returns the trace of any submission's current job, not only the
// graded one, fetching a fresh presigned URL when it is not cached yet.
func (l *Loader) JobTrace(ds *Dataset, sub forge.Submission) (*trace.Trace, error) {
	if sub.CurrentJob == nil || sub.CurrentJob.ID == "" {
		return nil, fmt.Errorf("this submission has no test job (%s)", strings.ToLower(orDefault(sub.Error, sub.Status)))
	}
	c := &Cache{Root: l.Cache.path(ds.Activity.Slug)}
	name := path.Join("traces", sub.CurrentJob.ID+".xml")
	if b, ok := c.ReadFile(name); ok {
		return trace.Parse(b)
	}
	urls, err := l.Client.TraceURLs(sub.SubmissionDefinitionURI, []string{sub.CurrentJob.ID})
	if err != nil {
		return nil, err
	}
	u := urls[sub.CurrentJob.ID]
	if u == "" {
		return nil, fmt.Errorf("no trace for this job (status %s)", strings.ToLower(sub.CurrentJob.Status))
	}
	b, err := l.Client.Download(u)
	if err != nil {
		return nil, err
	}
	_ = c.WriteFile(name, b)
	return trace.Parse(b)
}

func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
