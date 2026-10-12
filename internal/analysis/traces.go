// Package analysis finds suspiciously similar students.
package analysis

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"tp-inspector/internal/trace"
)

// SharedFailure is a test both students fail.
type SharedFailure struct {
	TestID  string
	SameMsg bool // identical (normalized) failure output
	DF      int  // how many students fail this test
}

// TracePair is a pair of students with similar failure patterns.
type TracePair struct {
	A, B       string
	Score      float64 // 0..1
	Jaccard    float64 // IDF-weighted Jaccard on failed tests
	SameMsg    int     // shared failures with identical output
	Shared     []SharedFailure
	FailA      int
	FailB      int
	Population int

	// The factors of Score, to explain it.
	Identical float64 // share of shared failures with an identical output, 0..1
	Evidence  float64 // 0..1, grows with the weight of shared uncommon failures
	Rare      int     // shared failures with DF <= RareMax(Population)
	RareSame  int     // rare shared failures with an identical output
}

// RareMax is the number of failing students up to which a failure is called
// rare in a class of n.
func RareMax(n int) int { return max(3, n/50) }

// Level is a coarse, readable reading of a pair's score.
type Level int

const (
	Low Level = iota
	Medium
	High
)

func (l Level) String() string {
	switch l {
	case High:
		return "high"
	case Medium:
		return "medium"
	}
	return "low"
}

// LevelOf maps a score to a level: high from 0.8, medium from 0.5.
func LevelOf(score float64) Level {
	switch {
	case score >= 0.8:
		return High
	case score >= 0.5:
		return Medium
	}
	return Low
}

// Level of the pair.
func (p TracePair) Level() Level { return LevelOf(p.Score) }

// Reason summarizes in words why the pair stands out.
func (p TracePair) Reason() string {
	if p.Rare == 0 {
		return plural(len(p.Shared), "shared fail", "shared fails") + ", " +
			plural(p.SameMsg, "identical output", "identical outputs")
	}
	return fmt.Sprintf("%s in common (≤%d students each), %s",
		plural(p.Rare, "rare fail", "rare fails"), RareMax(p.Population),
		plural(p.RareSame, "identical output", "identical outputs"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// EvidenceScale is the shared IDF weight at which a pair gets ~63% of its
// similarity as score (a rare test weighs ln(N/df) ≈ 2–5).
const EvidenceScale = 10.0

// TraceOptions tunes CompareTraces.
type TraceOptions struct {
	MinShared int     // minimum shared failed tests (default 2)
	MinScore  float64 // default 0.2
	MaxPairs  int     // default 2000
	// Per-exercise failure patterns (exact set of failed tests + outputs)
	// shared by more students than this are common mistakes, not copies.
	// Default: max(4, 1% of the population).
	MaxPatternShare int
}

// Informative returns the failed tests of a trace that carry signal. It drops
// build/architecture checks, failures against the testsuite's stub (file not
// submitted) and every test of an exercise the student failed almost entirely:
// not doing an exercise produces the same failures for everyone.
func Informative(t *trace.Trace) []trace.Test {
	total, failed := map[string]int{}, map[string]int{}
	for _, x := range t.Tests {
		if x.Skipped {
			continue
		}
		c := trace.Category(x.Class)
		total[c]++
		if !x.Passed {
			failed[c]++
		}
	}
	var out []trace.Test
	for _, x := range t.Tests {
		if x.Passed || x.Skipped || trace.IsGlobalCheck(x) ||
			strings.HasPrefix(x.Class, "Build.") || strings.HasPrefix(x.Class, "Architecture.") ||
			strings.Contains(x.Message, "/stub/") {
			continue
		}
		c := trace.Category(x.Class)
		if float64(failed[c]) >= 0.9*float64(total[c]) {
			continue
		}
		out = append(out, x)
	}
	return out
}

// CompareTraces scores every pair of students by how alike their failures
// are. Failing a test that almost nobody fails weighs a lot (IDF), and
// failing it with the exact same output weighs more.
func CompareTraces(traces map[string]*trace.Trace, opt TraceOptions) []TracePair {
	if opt.MinShared == 0 {
		opt.MinShared = 2
	}
	if opt.MinScore == 0 {
		opt.MinScore = 0.2
	}
	if opt.MaxPairs == 0 {
		opt.MaxPairs = 2000
	}

	logins := make([]string, 0, len(traces))
	for l := range traces {
		logins = append(logins, l)
	}
	sort.Strings(logins)
	n := len(logins)
	if n < 2 {
		return nil
	}

	type failure struct {
		who int
		msg string
	}
	failers := map[string][]failure{}
	if opt.MaxPatternShare == 0 {
		opt.MaxPatternShare = max(4, n/100)
	}
	// Group informative failures by exercise and fingerprint each pattern.
	perStudent := make([]map[string][]trace.Test, n)
	patternDF := map[string]int{}
	signature := func(cat string, ts []trace.Test) string {
		parts := make([]string, len(ts))
		for i, t := range ts {
			parts[i] = t.ID + "\x00" + t.Message
		}
		sort.Strings(parts)
		return cat + "\x01" + strings.Join(parts, "\x02")
	}
	for i, l := range logins {
		byCat := map[string][]trace.Test{}
		for _, t := range Informative(traces[l]) {
			c := trace.Category(t.Class)
			byCat[c] = append(byCat[c], t)
		}
		perStudent[i] = byCat
		for c, ts := range byCat {
			patternDF[signature(c, ts)]++
		}
	}
	informative := make([]int, n)
	for i := range logins {
		for c, ts := range perStudent[i] {
			if patternDF[signature(c, ts)] > opt.MaxPatternShare {
				continue
			}
			for _, t := range ts {
				failers[t.ID] = append(failers[t.ID], failure{i, t.Message})
				informative[i]++
			}
		}
	}
	idf := map[string]float64{}
	weight := make([]float64, n) // total weight of each student's failures
	for id, fs := range failers {
		w := math.Log(float64(n) / float64(len(fs)))
		idf[id] = w
		for _, f := range fs {
			weight[f.who] += w
		}
	}

	type acc struct {
		inter  float64
		same   int
		shared []SharedFailure
	}
	pairs := map[[2]int]*acc{}
	for id, fs := range failers {
		// A test failed by most of the promo carries no signal.
		if len(fs) < 2 || len(fs) > n/2 {
			continue
		}
		w := idf[id]
		for x := 0; x < len(fs); x++ {
			for y := x + 1; y < len(fs); y++ {
				k := [2]int{fs[x].who, fs[y].who}
				a := pairs[k]
				if a == nil {
					a = &acc{}
					pairs[k] = a
				}
				same := fs[x].msg != "" && fs[x].msg == fs[y].msg
				a.inter += w
				if same {
					a.same++
				}
				a.shared = append(a.shared, SharedFailure{TestID: id, SameMsg: same, DF: len(fs)})
			}
		}
	}

	var out []TracePair
	for k, a := range pairs {
		if len(a.shared) < opt.MinShared {
			continue
		}
		union := weight[k[0]] + weight[k[1]] - a.inter
		if union <= 0 {
			continue
		}
		j := a.inter / union
		// Few shared failures are weak evidence however similar: scale by
		// the mass of shared rare failures (≈3 rare tests -> ×0.63).
		evidence := 1 - math.Exp(-a.inter/EvidenceScale)
		score := j * (0.5 + 0.5*float64(a.same)/float64(len(a.shared))) * evidence
		if score < opt.MinScore {
			continue
		}
		sort.Slice(a.shared, func(i, j int) bool { return a.shared[i].DF < a.shared[j].DF })
		rare, rareSame := 0, 0
		for _, sf := range a.shared {
			if sf.DF <= RareMax(n) {
				rare++
				if sf.SameMsg {
					rareSame++
				}
			}
		}
		la, lb := logins[k[0]], logins[k[1]]
		out = append(out, TracePair{A: la, B: lb, Score: score, Jaccard: j, SameMsg: a.same, Shared: a.shared,
			FailA: informative[k[0]], FailB: informative[k[1]], Population: n,
			Identical: float64(a.same) / float64(len(a.shared)), Evidence: evidence, Rare: rare, RareSame: rareSame})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].A+out[i].B < out[j].A+out[j].B
	})
	if len(out) > opt.MaxPairs {
		out = out[:opt.MaxPairs]
	}
	return out
}
