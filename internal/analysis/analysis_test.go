package analysis

import (
	"fmt"
	"testing"

	"tp-inspector/internal/trace"
)

func mk(fails map[string]string) *trace.Trace {
	tr := &trace.Trace{}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("T::t%d", i)
		msg, failed := fails[id]
		tr.Tests = append(tr.Tests, trace.Test{ID: id, Passed: !failed, Message: msg})
		if failed {
			tr.Failed++
		}
	}
	return tr
}

func TestCompareTraces(t *testing.T) {
	traces := map[string]*trace.Trace{
		"a": mk(map[string]string{"T::t1": "got 4", "T::t2": "got 7", "T::t3": "x", "T::t4": "w", "T::t5": "v"}),
		"b": mk(map[string]string{"T::t1": "got 4", "T::t2": "got 7", "T::t3": "x", "T::t4": "w", "T::t5": "v"}),
		"c": mk(map[string]string{"T::t1": "got 3", "T::t9": "y"}),
		"d": mk(map[string]string{"T::t10": "z"}),
		"e": mk(nil), "f": mk(nil), "g": mk(nil), "h": mk(nil),
	}
	pairs := CompareTraces(traces, TraceOptions{})
	if len(pairs) != 1 || pairs[0].A != "a" || pairs[0].B != "b" || pairs[0].SameMsg != 5 {
		t.Fatalf("unexpected pairs: %+v", pairs)
	}
	p := pairs[0]
	if p.Rare != 5 || p.RareSame != 5 || p.Identical != 1 || p.Evidence <= 0 || p.Evidence >= 1 {
		t.Fatalf("unexpected factors: %+v", p)
	}
	if want := "5 rare fails in common (≤3 students each), 5 identical outputs"; p.Reason() != want {
		t.Fatalf("reason = %q, want %q", p.Reason(), want)
	}
}

func TestLevelOf(t *testing.T) {
	for _, c := range []struct {
		score float64
		want  Level
	}{{0.2, Low}, {0.49, Low}, {0.5, Medium}, {0.79, Medium}, {0.8, High}, {1, High}} {
		if got := LevelOf(c.score); got != c.want {
			t.Errorf("LevelOf(%v) = %v, want %v", c.score, got, c.want)
		}
	}
}
