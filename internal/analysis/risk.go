package analysis

import (
	"fmt"
	"math"
	"sort"
	"time"

	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
)

// Thresholds of the timing signals, as fractions of the submission window.
const (
	SoonFraction = 0.10 // reaching the final grade in the first 10% is suspicious
	LateFraction = 0.03 // reaching it in the last 3% is suspicious
	JumpHigh     = 90   // a first submission at least this high is a jump
	JumpMax      = 0.5  // a jump alone makes a 100% student medium, not high
)

// RiskScore says how likely a student is to have cheated from their
// submissions alone: a high grade reached very soon after the opening, at the
// last minute, or in one jump. A low grade is never risky.
type RiskScore struct {
	Score  float64 // Grade × (0.4 + 0.6 × min(1, Timing + Jump))
	Grade  float64 // (grade/100)², 100% stands out and 60% barely counts
	Timing float64 // 1 when the final grade was reached at the very start or end
	Jump   float64 // up to JumpMax when the final grade came without intermediate steps
	// Reasons explain the signals that fired, in words.
	Reasons []string
	// DoneAt is when the student first reached their final grade.
	DoneAt time.Time
}

// Level of the risk.
func (r RiskScore) Level() Level { return LevelOf(r.Score) }

// Window is the reference period of the timing signal.
type Window struct {
	Open, Close time.Time
	FromClass   bool // no window from the API: first and last submission of the class
}

// Fraction is where t falls in the window, 0 at the opening and 1 at the closing.
func (w Window) Fraction(t time.Time) float64 {
	d := w.Close.Sub(w.Open)
	if d <= 0 {
		return 0.5
	}
	return float64(t.Sub(w.Open)) / float64(d)
}

// WindowFor returns the API window when known, else the span of the class's
// submissions to sd.
func WindowFor(students []*data.Student, sd string, api *forge.SubmissionWindow) Window {
	if api != nil && api.Valid() {
		return Window{Open: api.OpeningAt, Close: api.ClosingAt}
	}
	w := Window{FromClass: true}
	for _, st := range students {
		for _, s := range st.SubsFor(sd) {
			if w.Open.IsZero() || s.ReceivedAt.Before(w.Open) {
				w.Open = s.ReceivedAt
			}
			if s.ReceivedAt.After(w.Close) {
				w.Close = s.ReceivedAt
			}
		}
	}
	return w
}

// Risk scores every student with a graded submission to sd.
func Risk(students []*data.Student, sd string, win Window) map[string]RiskScore {
	// All-or-nothing checks (archi: 0% or 100%) jump by nature.
	binary, graded := 0, 0
	for _, st := range students {
		for _, s := range st.SubsFor(sd) {
			if p := s.Percent(); p >= 0 {
				graded++
				if p == 0 || p == 100 {
					binary++
				}
			}
		}
	}
	jumps := graded == 0 || float64(binary) < 0.9*float64(graded)
	out := map[string]RiskScore{}
	for _, st := range students {
		if r, ok := riskOf(st, sd, win, jumps); ok {
			out[st.Login] = r
		}
	}
	return out
}

func riskOf(st *data.Student, sd string, win Window, jumps bool) (RiskScore, bool) {
	pct := st.Percent(sd)
	if pct < 0 || st.Last(sd) == nil {
		return RiskScore{}, false
	}
	r := RiskScore{Grade: math.Pow(float64(pct)/100, 2)}

	// Chronological percentages of the graded submissions.
	subs := st.SubsFor(sd)
	sort.Slice(subs, func(i, j int) bool { return subs[i].ReceivedAt.Before(subs[j].ReceivedAt) })
	type step struct {
		at  time.Time
		pct int
	}
	var steps []step
	for _, s := range subs {
		if p := s.Percent(); p >= 0 {
			steps = append(steps, step{s.ReceivedAt, p})
		}
	}
	if len(steps) == 0 {
		steps = []step{{st.Last(sd).ReceivedAt, pct}}
	}
	for _, s := range steps {
		if s.pct >= pct {
			r.DoneAt = s.at
			break
		}
	}
	if r.DoneAt.IsZero() {
		r.DoneAt = steps[len(steps)-1].at
	}

	ref := "the window"
	if win.FromClass {
		ref = "the promo's submissions"
	}
	f := win.Fraction(r.DoneAt)
	soon := clamp01(1 - f/SoonFraction)
	late := clamp01(1 - (1-f)/LateFraction)
	r.Timing = math.Max(soon, late)
	switch {
	case pct > 0 && soon > 0:
		r.Reasons = append(r.Reasons, fmt.Sprintf("%d%% reached %s after the opening (first %d%% of %s)",
			pct, human(r.DoneAt.Sub(win.Open)), max(1, int(math.Ceil(f*100))), ref))
	case pct > 0 && late > 0 && f > 1:
		r.Reasons = append(r.Reasons, fmt.Sprintf("%d%% reached %s after the closing", pct, human(r.DoneAt.Sub(win.Close))))
	case pct > 0 && late > 0:
		r.Reasons = append(r.Reasons, fmt.Sprintf("%d%% reached %s before the closing (last %d%% of %s)",
			pct, human(win.Close.Sub(r.DoneAt)), max(1, int(math.Ceil((1-f)*100))), ref))
	}

	if !jumps {
		// no jump signal
	} else if first := steps[0]; first.pct >= JumpHigh && pct >= JumpHigh {
		r.Jump = JumpMax
		if len(steps) == 1 {
			r.Reasons = append(r.Reasons, fmt.Sprintf("only one submission, already %d%%", first.pct))
		} else {
			r.Reasons = append(r.Reasons, fmt.Sprintf("first submission already %d%%", first.pct))
		}
	} else {
		best, from, to := 0.0, 0, 0
		for i := 1; i < len(steps); i++ {
			d := steps[i].pct - steps[i-1].pct
			if steps[i].pct < 80 {
				continue
			}
			// +30 points is ordinary work, +80 at once is a jump.
			if j := clamp01(float64(d-30) / 50); j > best {
				best, from, to = j, steps[i-1].pct, steps[i].pct
			}
		}
		if best > 0 {
			r.Jump = best * JumpMax
			r.Reasons = append(r.Reasons, fmt.Sprintf("jumped from %d%% to %d%% in one submission", from, to))
		}
	}
	r.Score = r.Grade * (0.4 + 0.6*math.Min(1, r.Timing+r.Jump))
	return r, true
}

// human formats a duration coarsely: 45m, 3h, 2d.
func human(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
