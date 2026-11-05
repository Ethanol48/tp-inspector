package analysis

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
)

// Thresholds of the timing and jump signals.
const (
	SoonFraction = 0.10 // reaching the final grade in the first 10% of the window is suspicious...
	LateFraction = 0.03 // ...and so is reaching it in the last 3%, but less (see LateWeight)
	JumpHigh     = 90   // a first submission at least this high is a jump
	JumpMax      = 0.5  // a jump alone makes a 100% student medium, not high

	// SoonMaxDuration also bounds "soon" in absolute time: 10% of a three-week
	// window is two days of work, which proves nothing.
	SoonMaxDuration = 48 * time.Hour
	// LateWeight: working until the last minute is what most students do, so
	// it counts half as much as an immediate finish.
	LateWeight = 0.5
	// ClassWindowWeight: when the API gives no window, the class's first and
	// last submissions stand in for it. The first student is then at 0% of
	// the window by construction, so the timing counts half as much.
	ClassWindowWeight = 0.5
	// MinClassSpan: a class window shorter than this says nothing about timing.
	MinClassSpan = 12 * time.Hour
	// A final grade reached after many submissions is evidence of real work:
	// each extra submission before it lowers the timing by IterStep, down to
	// IterFloor.
	IterStep  = 0.15
	IterFloor = 0.4

	riskHit       = 0.5  // a raw signal this strong counts as "the student does it" in the class rates
	minRiskReason = 0.05 // a signal weaker than this (after weighting) is not worth a reason
)

// RiskScore says how likely a student is to have cheated from their
// submissions alone: a high grade reached very soon after the opening, at the
// last minute, or in one jump. A low grade is never risky. Every signal is
// weighted by how much the window can be trusted, by the work visible in the
// submissions and by how rare it is in the class.
type RiskScore struct {
	Score  float64 // Grade × (0.4 + 0.6 × min(1, Timing + Jump))
	Grade  float64 // (grade/100)², 100% stands out and 60% barely counts
	Timing float64 // up to 1 when the final grade was reached at the very start (at the very end: up to LateWeight)
	Jump   float64 // up to JumpMax when the final grade came without intermediate steps
	// Reasons explain the signals that fired, in words, strongest first.
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

// Weight is how far the timing signals can be trusted: 1 for the API window,
// less for a window rebuilt from the class's submissions, 0 when that span is
// too short to mean anything.
func (w Window) Weight() float64 {
	if !w.FromClass {
		return 1
	}
	if w.Close.Sub(w.Open) < MinClassSpan {
		return 0
	}
	return ClassWindowWeight
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
			if s.ReceivedAt.IsZero() {
				continue
			}
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

// Risk scores every student with a graded submission to sd. Signals that
// many classmates show too (an easy subject, a window opened long before the
// class started) are discounted: only what is rare in the class stands out.
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

	var ins []*riskInput
	for _, st := range students {
		if in := newRiskInput(st, sd, win); in != nil {
			ins = append(ins, in)
		}
	}
	rates := classRates(ins)
	out := make(map[string]RiskScore, len(ins))
	for _, in := range ins {
		out[in.login] = in.score(win, jumps, rates)
	}
	return out
}

// ---- raw signals of one student ---------------------------------------------

type riskStep struct {
	at  time.Time
	pct int
}

const (
	jumpNone  = iota
	jumpFirst // the first submission is already high
	jumpStep  // one submission gained many points
)

// riskInput holds the raw signals of one student, before any weighting.
type riskInput struct {
	login  string
	pct    int
	steps  []riskStep // graded submissions, oldest first
	done   int        // index in steps of the first one reaching pct
	doneAt time.Time
	f      float64 // where doneAt falls in the window

	soon, late float64 // 0..1, raw
	iter       float64 // IterFloor..1: 1 when the grade came in a single submission

	jump             float64 // 0..1, raw
	jumpKind         int
	jumpFrom, jumpTo int
}

// newRiskInput reads the submissions of a student; nil when there is no
// graded submission.
func newRiskInput(st *data.Student, sd string, win Window) *riskInput {
	pct := st.Percent(sd)
	last := st.Last(sd)
	if pct < 0 || last == nil {
		return nil
	}

	// Chronological percentages of the graded submissions. The slice is
	// copied: sorting must not reorder the student's own list.
	subs := slices.Clone(st.SubsFor(sd))
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].ReceivedAt.Before(subs[j].ReceivedAt) })
	var steps []riskStep
	for _, s := range subs {
		if p := s.Percent(); p >= 0 {
			steps = append(steps, riskStep{s.ReceivedAt, p})
		}
	}
	if len(steps) == 0 {
		steps = []riskStep{{last.ReceivedAt, pct}}
	}
	done := len(steps) - 1
	for i, s := range steps {
		if s.pct >= pct {
			done = i
			break
		}
	}
	in := &riskInput{login: st.Login, pct: pct, steps: steps, done: done, doneAt: steps[done].at}

	// Timing. "Soon" must hold both as a share of the window and in absolute
	// time; after the closing nothing is "late" (that is a late submission,
	// not a last-minute one).
	in.f = win.Fraction(in.doneAt)
	elapsed := in.doneAt.Sub(win.Open)
	soonFrac := clamp01(1 - in.f/SoonFraction)
	soonAbs := clamp01(1 - float64(elapsed)/float64(SoonMaxDuration))
	in.soon = math.Min(soonFrac, soonAbs)
	if in.f <= 1 {
		in.late = clamp01(1 - (1-in.f)/LateFraction)
	}
	in.iter = math.Max(IterFloor, 1-IterStep*float64(done))

	// Jump.
	if first := steps[0]; first.pct >= JumpHigh && pct >= JumpHigh {
		// 90% as a first try is a good start, 100% is a finished solution.
		in.jump = clamp01(float64(first.pct-80) / 20)
		in.jumpKind, in.jumpTo = jumpFirst, first.pct
	} else {
		for i := 1; i < len(steps); i++ {
			if steps[i].pct < 80 {
				continue
			}
			d := steps[i].pct - steps[i-1].pct
			// +30 points is ordinary work, +80 at once is a jump.
			if j := clamp01(float64(d-30) / 50); j > in.jump {
				in.jump = j
				in.jumpKind, in.jumpFrom, in.jumpTo = jumpStep, steps[i-1].pct, steps[i].pct
			}
		}
	}
	return in
}

// ---- the class as baseline ------------------------------------------------------

// riskRates counts the students who show each raw signal.
type riskRates struct {
	n                 int // students with a grade above 0
	soon, late, jumps int
}

func classRates(ins []*riskInput) riskRates {
	var r riskRates
	for _, in := range ins {
		if in.pct <= 0 {
			continue
		}
		r.n++
		if in.soon >= riskHit {
			r.soon++
		}
		if in.late >= riskHit {
			r.late++
		}
		if in.jump >= riskHit {
			r.jumps++
		}
	}
	return r
}

// rarity says how much a signal still counts given the share of the other
// students showing it: 1 when nobody does, 0 when everybody does. own tells
// whether the student is one of the hits. Below MinBaseline students the class
// says nothing and the signal counts in full.
func (r riskRates) rarity(hits int, own bool) (rar, share float64) {
	if r.n < MinBaseline {
		return 1, 0
	}
	if own {
		hits--
	}
	share = clamp01(float64(hits) / float64(r.n-1))
	return (1 - share) * (1 - share), share
}

// ---- score ----------------------------------------------------------------------

func (in *riskInput) score(win Window, jumps bool, rates riskRates) RiskScore {
	r := RiskScore{Grade: math.Pow(float64(in.pct)/100, 2), DoneAt: in.doneAt}
	if in.pct <= 0 {
		return r
	}

	type why struct {
		w float64
		s string
	}
	var reasons []why
	// common tells the reader that a signal was discounted.
	common := func(share float64) string {
		if share < 0.2 {
			return ""
		}
		return fmt.Sprintf(" (common: %.0f%% of the promo)", share*100)
	}

	ref := "the window"
	if win.FromClass {
		ref = "the promo's submissions"
	}
	subs := plural(in.done+1, "submission", "submissions")
	weight := win.Weight()

	rarSoon, shSoon := rates.rarity(rates.soon, in.soon >= riskHit)
	rarLate, shLate := rates.rarity(rates.late, in.late >= riskHit)
	soon := in.soon * weight * rarSoon * in.iter
	late := in.late * LateWeight * weight * rarLate * in.iter
	r.Timing = math.Max(soon, late)

	if soon >= minRiskReason {
		s := ""
		if in.f < 0 {
			s = fmt.Sprintf("%d%% reached %s before the opening, in %s",
				in.pct, human(win.Open.Sub(in.doneAt)), subs)
		} else {
			s = fmt.Sprintf("%d%% reached %s after the opening, in %s (first %d%% of %s)",
				in.pct, human(in.doneAt.Sub(win.Open)), subs, max(1, int(math.Ceil(in.f*100))), ref)
		}
		reasons = append(reasons, why{soon, s + common(shSoon)})
	}
	if late >= minRiskReason {
		s := fmt.Sprintf("%d%% reached %s before the closing, in %s (last %d%% of %s)",
			in.pct, human(win.Close.Sub(in.doneAt)), subs, max(1, int(math.Ceil((1-in.f)*100))), ref)
		reasons = append(reasons, why{late, s + common(shLate)})
	}

	if jumps && in.jumpKind != jumpNone {
		rarJump, shJump := rates.rarity(rates.jumps, in.jump >= riskHit)
		r.Jump = in.jump * JumpMax * rarJump
		if r.Jump >= minRiskReason {
			s := ""
			switch {
			case in.jumpKind == jumpFirst && len(in.steps) == 1:
				s = fmt.Sprintf("only one submission, already %d%%", in.jumpTo)
			case in.jumpKind == jumpFirst:
				s = fmt.Sprintf("first submission already %d%%", in.jumpTo)
			default:
				s = fmt.Sprintf("jumped from %d%% to %d%% in one submission", in.jumpFrom, in.jumpTo)
			}
			reasons = append(reasons, why{r.Jump, s + common(shJump)})
		}
	}

	sort.SliceStable(reasons, func(i, j int) bool { return reasons[i].w > reasons[j].w })
	for _, x := range reasons {
		r.Reasons = append(r.Reasons, x.s)
	}
	r.Score = r.Grade * (0.4 + 0.6*math.Min(1, r.Timing+r.Jump))
	return r
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