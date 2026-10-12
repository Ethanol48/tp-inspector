package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tp-inspector/internal/analysis"
	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
	"tp-inspector/internal/tp"
	"tp-inspector/internal/trace"
)

const copied = `
int find_max(int *values, int count)
{
    int best = values[0];
    for (int k = 1; k < count; k++)
    {
        if (values[k] > best)
            best = values[k];
    }
    while (count > 100) { count = count / 2; best = best + count; }
    return best * 2 + count - 1;
}
`

// TestViews renders every tab and the student and comparison views with
// risk, code and AI results, to catch panics and missing wiring.
func TestViews(t *testing.T) {
	root := t.TempDir()
	open := time.Now().Add(-7 * 24 * time.Hour)
	ds := &data.Dataset{Activity: forge.Activity{Slug: "tp"}, SDs: []string{"submit"}, SDURIs: map[string]string{"submit": "a/submit"},
		Windows: map[string]forge.SubmissionWindow{"submit": {OpeningAt: open, ClosingAt: open.Add(7 * 24 * time.Hour)}},
		ByLogin: map[string]*data.Student{}}
	dirs := map[string]string{}
	for i, l := range []string{"alice", "bob", "carol"} {
		p := 100 - 30*i
		st := &data.Student{Login: l, Picked: map[string]*forge.PickedSubmission{}, Group: forge.Group{GitRemoteURL: "git@forge:" + l},
			Subs: []forge.Submission{{ID: l, Ref: "submit-1", SubmissionDefinitionURI: "a/submit", Status: "SUCCEEDED",
				ReceivedAt: open.Add(time.Duration(i+1) * time.Hour), CurrentJob: &forge.Job{SuccessPercent: &p}}}}
		ds.Students = append(ds.Students, st)
		dir := filepath.Join(root, "tp", "repos", l)
		src := copied
		if l == "carol" {
			src = "int other(int a) { return a + 1; }\n"
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		dirs[l] = dir
	}
	m := New(tp.Slug{Raw: "tp"}, &data.Loader{Cache: &data.Cache{Root: root}}, []string{"alice"})
	m.ds, m.sd, m.w, m.h, m.gen = ds, "submit", 160, 50, 1 // as after loading
	if got := strings.Join(m.classToClone(), ","); got != "alice" {
		t.Errorf("A must clone the --logins list, got %q", got)
	}
	m.marked["carol"] = true
	if got := strings.Join(m.classToClone(), ","); got != "alice,carol" {
		t.Errorf("A must clone the list + marked, got %q", got)
	}
	m.list = map[string]bool{}
	if got := len(m.classToClone()); got != 3 {
		t.Errorf("without a list A clones the promotion, got %d", got)
	}
	m.marked = map[string]bool{}
	m.list = map[string]bool{"alice": true}
	if _, ok := m.suspicionOf("alice"); ok {
		t.Error("suspicion must stay unknown until the repo is analysed (it would only repeat the risk)")
	}
	m.code = analysis.Analyze(dirs, nil)
	m.codeBest = m.code.Best()
	if s, ok := m.suspicionOf("alice"); !ok || s < m.riskOf("alice") {
		t.Errorf("suspicion after analysis: %v %v (risk %v)", s, ok, m.riskOf("alice"))
	}
	if len(m.codePairs()) != 1 {
		t.Fatalf("pairs: %+v", m.codePairs())
	}
	if r := m.riskScores()["alice"]; r.Level() != analysis.High {
		t.Errorf("alice: 100%% one hour after opening at once should be high: %+v", r)
	}
	for i := range m.tabs {
		m.active = i
		if out := m.View(); out == "" {
			t.Errorf("tab %s renders nothing", m.tabs[i].Title())
		}
	}
	m.active = 4 // Code
	if out := m.View(); !strings.Contains(out, "alice") || !strings.Contains(out, "find_max") {
		t.Errorf("Code tab misses the pair:\n%s", out)
	}
	d := newDetailView("alice")
	d.sub = 2
	m.push(d)
	if out := m.View(); !strings.Contains(out, "bob") || !strings.Contains(out, "reached") {
		t.Errorf("Suspects misses the code pair or the risk:\n%s", out)
	}
	m.push(newCompareView("bob", "alice"))
	if out := m.View(); !strings.Contains(out, "B find_max") || strings.Count(out, "main.c:2") < 4 || !strings.Contains(out, "best = values[0]") {
		t.Errorf("comparison misses the side-by-side code:\n%s", out)
	}
}

func TestCombine(t *testing.T) {
	if got := combine(0.5, 0.5, 0); got != 0.75 {
		t.Errorf("combine(0.5, 0.5, 0) = %v, want 0.75", got)
	}
	if got := combine(0.4, 0, 0); got != 0.4 {
		t.Errorf("a single signal must be kept: %v", got)
	}
}

// An exam grades submission-* tags: the student page must show and open
// their trace like a TP's submit-* ones.
func TestExamTrace(t *testing.T) {
	b, err := os.ReadFile("../trace/testdata/sample.xml")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := trace.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	p := 99
	sub := forge.Submission{ID: "s1", Ref: "submission-abc1234", SubmissionDefinitionURI: "a/exam/submission", Status: "SUCCEEDED",
		ReceivedAt: time.Now(), CurrentJob: &forge.Job{ID: "j1", SuccessPercent: &p}}
	st := &data.Student{Login: "eve", Subs: []forge.Submission{sub},
		Picked: map[string]*forge.PickedSubmission{"submission": {SubmissionID: "s1", CurrentJobID: "j1"}},
		Traces: map[string]*trace.Trace{"submission": tr}}
	ds := &data.Dataset{Activity: forge.Activity{Slug: "exam"}, SDs: []string{"submission"},
		SDURIs: map[string]string{"submission": "a/exam/submission"}, Students: []*data.Student{st}, ByLogin: map[string]*data.Student{}}
	m := New(tp.Slug{Raw: "exam"}, &data.Loader{Cache: &data.Cache{Root: t.TempDir()}}, nil)
	m.ds, m.sd, m.w, m.h, m.gen = ds, "submission", 160, 50, 1
	m.push(newDetailView("eve"))
	if out := m.View(); strings.Contains(out, "No graded") || !strings.Contains(out, "submission-abc1234") {
		t.Errorf("exam trace not shown:\n%s", out)
	}
}

// Risk sorts everyone; suspicion puts analysed students first, then the
// others by risk.
func TestRankingSorts(t *testing.T) {
	open := time.Now().Add(-7 * 24 * time.Hour)
	ds := &data.Dataset{SDs: []string{"submit"}, SDURIs: map[string]string{"submit": "a/submit"},
		Windows: map[string]forge.SubmissionWindow{"submit": {OpeningAt: open, ClosingAt: open.Add(7 * 24 * time.Hour)}}}
	for _, s := range []struct {
		login string
		hours int
		pct   int
	}{{"early", 1, 100}, {"mid", 80, 100}, {"weak", 90, 30}} {
		p := s.pct
		ds.Students = append(ds.Students, &data.Student{Login: s.login, Picked: map[string]*forge.PickedSubmission{},
			Subs: []forge.Submission{{ID: s.login, Ref: "submit-1", SubmissionDefinitionURI: "a/submit",
				ReceivedAt: open.Add(time.Duration(s.hours) * time.Hour), CurrentJob: &forge.Job{SuccessPercent: &p}}}})
	}
	m := New(tp.Slug{Raw: "tp"}, &data.Loader{Cache: &data.Cache{Root: t.TempDir()}}, nil)
	m.ds, m.sd, m.gen = ds, "submit", 1
	// Only "weak" has an analysed repo, with a strong code pair.
	m.code = &analysis.CodeResult{Repos: map[string]*analysis.Repo{"weak": {}}, AI: map[string]analysis.AIScore{}}
	m.codeBest = map[string]float64{"weak": 0.9}
	order := func(mode string) string {
		rv := newRankingView()
		rv.hidden = map[string]bool{}
		for i, s := range rankingSorts {
			if s == mode {
				rv.sortMode = i
			}
		}
		rv.rebuild(m)
		var ks []string
		for _, r := range rv.t.Rows {
			ks = append(ks, r.Key)
		}
		return strings.Join(ks, ",")
	}
	if got := order("risk"); got != "early,mid,weak" {
		t.Errorf("risk order: %s", got)
	}
	if got := order("suspicion"); got != "weak,early,mid" {
		t.Errorf("suspicion order (analysed first, then by risk): %s", got)
	}
}
