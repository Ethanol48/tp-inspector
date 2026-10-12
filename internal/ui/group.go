package ui

import (
	"fmt"
	"math"
	"sort"

	"tp-inspector/internal/analysis"
	"tp-inspector/internal/data"
	"tp-inspector/internal/trace"
)

// group returns the students of interest: the --logins list plus the
// students marked by hand, restricted to those in the assignment.
func (m *Model) group() map[string]bool {
	out := map[string]bool{}
	for _, set := range []map[string]bool{m.list, m.marked} {
		for l, ok := range set {
			if ok && m.student(l) != nil {
				out[l] = true
			}
		}
	}
	return out
}

func (m *Model) hasGroup() bool { return len(m.group()) > 0 }

// groupStudents returns the students of the group, in dataset order.
func (m *Model) groupStudents() []*data.Student {
	g := m.group()
	var out []*data.Student
	for _, st := range m.ds.Students {
		if g[st.Login] {
			out = append(out, st)
		}
	}
	return out
}

// gradeStats summarizes the grades of a set of students.
type gradeStats struct {
	n, delivered, missing, errors, full int
	mean, median                        float64
	percents                            []int // sorted
}

func computeStats(students []*data.Student, sd string) gradeStats {
	s := gradeStats{n: len(students)}
	for _, st := range students {
		last := st.Last(sd)
		if last == nil {
			continue
		}
		s.delivered++
		if last.Status == "ERROR" {
			s.errors++
		}
		if p := st.Percent(sd); p >= 0 {
			s.percents = append(s.percents, p)
			if p == 100 {
				s.full++
			}
		}
	}
	s.missing = s.n - s.delivered
	sort.Ints(s.percents)
	if k := len(s.percents); k > 0 {
		for _, p := range s.percents {
			s.mean += float64(p)
		}
		s.mean /= float64(k)
		if k%2 == 1 {
			s.median = float64(s.percents[k/2])
		} else {
			s.median = float64(s.percents[k/2-1]+s.percents[k/2]) / 2
		}
	}
	return s
}

// quartile returns the grade below which a quarter of the sorted grades lie.
func quartile(sorted []int) int {
	if len(sorted) == 0 {
		return -1
	}
	return sorted[len(sorted)/4]
}

// percentile returns the share (0..100) of the sorted class grades strictly
// below p.
func percentile(p int, sorted []int) int {
	if len(sorted) == 0 {
		return 0
	}
	below := sort.SearchInts(sorted, p)
	return int(math.Round(100 * float64(below) / float64(len(sorted))))
}

// rate is a pass count over a total.
type rate struct{ pass, total int }

func (r rate) pct() int {
	if r.total == 0 {
		return -1
	}
	return int(math.Round(100 * float64(r.pass) / float64(r.total)))
}

// categoryRates returns the pass rate of each exercise for the students in
// only (every student when nil), without global checks.
func (m *Model) categoryRates(sd string, only map[string]bool) map[string]rate {
	out := map[string]rate{}
	for login, tr := range m.tracesFor(sd) {
		if only != nil && !only[login] {
			continue
		}
		for _, t := range tr.Tests {
			if t.Skipped || trace.IsGlobalCheck(t) {
				continue
			}
			c := trace.Category(t.Class)
			r := out[c]
			r.total++
			if t.Passed {
				r.pass++
			}
			out[c] = r
		}
	}
	return out
}

// pairInvolves reports whether a pair has a student of the set.
func pairInvolves(p analysis.TracePair, set map[string]bool) bool { return set[p.A] || set[p.B] }

// deltaCell renders a difference in points, red when negative.
func deltaCell(d float64, unit string) string {
	txt := fmt.Sprintf("%+.1f%s", d, unit)
	switch {
	case d <= -0.05:
		return sBad.Render(txt)
	case d >= 0.05:
		return sGood.Render(txt)
	}
	return sDim.Render(txt)
}
