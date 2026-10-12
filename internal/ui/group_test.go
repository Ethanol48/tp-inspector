package ui

import (
	"testing"

	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
	"tp-inspector/internal/trace"
)

func graded(login string, pct int) *data.Student {
	return &data.Student{Login: login, Subs: []forge.Submission{{ID: login, SubmissionDefinitionURI: "x/submit", Status: "SUCCEEDED"}},
		Traces: map[string]*trace.Trace{"submit": {Percent: pct}}}
}

func TestComputeStats(t *testing.T) {
	errored := graded("e", 0)
	errored.Subs[0].Status = "ERROR"
	errored.Traces = nil
	sts := []*data.Student{graded("a", 40), graded("b", 100), graded("c", 60), graded("d", 80), {Login: "m"}, errored}
	s := computeStats(sts, "submit")
	if s.n != 6 || s.delivered != 5 || s.missing != 1 || s.errors != 1 || s.full != 1 {
		t.Fatalf("counts: %+v", s)
	}
	if s.mean != 70 || s.median != 70 {
		t.Fatalf("mean %v median %v, want 70 70", s.mean, s.median)
	}
	s = computeStats(sts[:3], "submit")
	if s.median != 60 {
		t.Fatalf("odd median %v, want 60", s.median)
	}
	if s = computeStats(nil, "submit"); s.mean != 0 || s.median != 0 {
		t.Fatalf("empty: %+v", s)
	}
}

func TestPercentile(t *testing.T) {
	class := []int{10, 20, 20, 50, 90}
	for _, c := range []struct{ p, want int }{{0, 0}, {20, 20}, {21, 60}, {100, 100}} {
		if got := percentile(c.p, class); got != c.want {
			t.Errorf("percentile(%d) = %d, want %d", c.p, got, c.want)
		}
	}
	if q := quartile(class); q != 20 {
		t.Errorf("quartile = %d, want 20", q)
	}
}
