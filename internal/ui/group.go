package ui

import (
	"fmt"
	"math"
	"strings"

	"tp-inspector/internal/analysis"
	"tp-inspector/internal/data"
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

// pairInvolves reports whether a pair has a student of the set.
func pairInvolves(a, b string, set map[string]bool) bool { return set[a] || set[b] }

// scorePct formats a 0..1 score, truncated so that it agrees with the level
// thresholds (0.799 is "79%", medium).
func scorePct(s float64) string { return fmt.Sprintf("%d%%", int(s*100)) }

// levelCell renders the level of a suspicion score, "·" when there is none.
func levelCell(score float64) string {
	if score <= 0 {
		return sDim.Render("·")
	}
	l := analysis.LevelOf(score)
	switch l {
	case analysis.High:
		return sBad.Bold(true).Render(l.String())
	case analysis.Medium:
		return sWarn.Render(l.String())
	}
	return sDim.Render(l.String())
}

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

func factorRow(name, val, desc string) string {
	return sBold.Render(padRight(name, 11)) + padRight(val, 7) + sDim.Render(desc)
}

func pct0(f float64) string { return fmt.Sprintf("%3d%%", int(math.Round(f*100))) }

// codeWhyLines explains the score of a code pair, one factor per line.
func codeWhyLines(p analysis.CodePair) []string {
	return []string{
		factorRow("Similarity", pct0(p.Sim), fmt.Sprintf("how alike the matched functions are, renaming and layout ignored (%d of A's %d functions, %d of B's %d)",
			len(p.Matches), p.FuncsA, len(p.Matches), p.FuncsB)),
		factorRow("Evidence", pct0(p.Evidence), fmt.Sprintf("%d tokens of matched code: one short function alike proves little", p.Matched)),
		factorRow("Identical", pct0(p.Identical), fmt.Sprintf("%d function bodies identical as written, variable names included (only whitespace and comments differ)", p.Same)),
		factorRow("Level", levelCell(p.Score), sDim.Render(fmt.Sprintf("score %s = 1 − (1 − similarity × evidence)(1 − identical); code found in more than %d of the %d repos (skeleton) is ignored",
			scorePct(p.Score), analysis.CommonMax(p.Repos), p.Repos))),
	}
}

// riskLines explains a risk score.
func riskLines(m *Model, r analysis.RiskScore) []string {
	w := m.window()
	ref := fmt.Sprintf("window %s → %s", fmtTime(w.Open), fmtTime(w.Close))
	if w.FromClass {
		ref = "no window from the API: the promo's first and last submissions stand in for it"
	}
	reasons := strings.Join(r.Reasons, "; ")
	if reasons == "" {
		reasons = "no timing or jump signal"
	}
	return []string{
		factorRow("Grade", pct0(r.Grade), "(grade/100)²: only high grades can be risky"),
		factorRow("Timing", pct0(r.Timing), fmt.Sprintf("final grade reached in the first %d%% or last %d%% of the %s",
			int(analysis.SoonFraction*100), int(analysis.LateFraction*100), ref)),
		factorRow("Jump", pct0(r.Jump), "final grade reached without intermediate submissions (at most 50%)"),
		factorRow("Level", levelCell(r.Score), sDim.Render("score "+scorePct(r.Score)+" = grade × (0.4 + 0.6 × min(1, timing + jump)) · "+reasons)),
	}
}

// aiLines explains an AI score.
func aiLines(s analysis.AIScore) []string {
	out := []string{factorRow("Level", levelCell(s.Score), sDim.Render(fmt.Sprintf("score %s: style vs promo %s, absolute tells %s",
		scorePct(s.Score), scorePct(s.Style), scorePct(s.Tells))))}
	for _, sg := range s.Signals {
		z := ""
		if sg.Z > 0 {
			z = fmt.Sprintf("+%.1fσ", sg.Z)
		}
		out = append(out, factorRow(sg.Name, z, sg.Detail))
	}
	return out
}
