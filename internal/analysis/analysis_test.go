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
}
