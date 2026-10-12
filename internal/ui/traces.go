package ui

import (
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/analysis"
)

type tracesView struct {
	t     Table
	gen   int
	pairs map[string]analysis.TracePair
}

func (v *tracesView) Title() string { return "Traces" }
func (v *tracesView) Keys() []string {
	return []string{keyHint("enter", "compare")}
}

func (v *tracesView) rebuild(m *Model) {
	v.t.Cols = []Column{
		{Title: "#", Width: 4, Right: true},
		{Title: "Level", Width: 6},
		{Title: "Student A", Width: 24},
		{Title: "Student B", Width: 24},
		{Title: "Why", Width: 0},
		{Title: "Score", Width: 5, Right: true},
		{Title: "Grades", Width: 9, Right: true},
	}
	v.pairs = map[string]analysis.TracePair{}
	var rows []Row
	for _, p := range m.pairsTraces() {
		pa, pb := -1, -1
		if st := m.student(p.A); st != nil {
			pa = st.Percent(m.sd)
		}
		if st := m.student(p.B); st != nil {
			pb = st.Percent(m.sd)
		}
		key := p.A + "\x00" + p.B
		v.pairs[key] = p
		rows = append(rows, Row{Key: key, Cells: []string{
			sDim.Render(fmt.Sprint(len(rows) + 1)), levelCell(p.Score), p.A, p.B, p.Reason(),
			sDim.Render(scorePct(p.Score)), pctCell(pa) + sDim.Render("/") + pctCell(pb),
		}})
	}
	v.t.Empty = "No pair with similar failures (needs ≥2 shared uncommon failures)."
	v.t.SetRows(rows)
}

func (v *tracesView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.t.HandleKey(k.String()) {
		return nil
	}
	if k.String() == "enter" {
		if sel := v.t.Selected(); sel != nil {
			p := v.pairs[sel.Key]
			m.push(newCompareView(p.A, p.B))
		}
	}
	return nil
}

func (v *tracesView) View(m *Model, w, h int) string {
	if v.gen != m.gen {
		v.rebuild(m)
		v.gen = m.gen
	}
	head := sDim.Render("Pairs failing the same tests that few others fail, often with the same output. Hints only: read the code before concluding.")
	head += "\n\n"
	why := ""
	if sel := v.t.Selected(); sel != nil {
		p := v.pairs[sel.Key]
		why = "\n" + sSectionHdr.Render("Why "+p.A+" & "+p.B) + "\n" +
			lipgloss.NewStyle().MaxWidth(w).Render(strings.Join(whyLines(p), "\n"))
	}
	used := strings.Count(head, "\n") + strings.Count(why, "\n")
	return head + v.t.View(w, max(3, h-used)) + why
}

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

// whyLines explains the score of a pair, one factor per line.
func whyLines(p analysis.TracePair) []string {
	pct := func(f float64) string { return fmt.Sprintf("%3d%%", int(math.Round(f*100))) }
	ev := "low"
	switch {
	case p.Evidence >= 0.8:
		ev = "high"
	case p.Evidence >= 0.5:
		ev = "medium"
	}
	row := func(name, val, desc string) string {
		return sBold.Render(padRight(name, 10)) + padRight(val, 7) + sDim.Render(desc)
	}
	return []string{
		row("Overlap", pct(p.Jaccard), fmt.Sprintf("how much their failures are the same, rare tests counting more (%d shared; A %d, B %d informative fails)", len(p.Shared), p.FailA, p.FailB)),
		row("Identical", pct(p.Identical), fmt.Sprintf("%d of the %d shared fails print exactly the same output", p.SameMsg, len(p.Shared))),
		row("Evidence", ev, fmt.Sprintf("%d shared fails, %d of them rare (failed by ≤ %d of %d students); few shared fails prove little", len(p.Shared), p.Rare, analysis.RareMax(p.Population), p.Population)),
		row("Level", levelCell(p.Score), sDim.Render("score "+scorePct(p.Score)+" = overlap × (½ + ½ identical) × evidence; high ≥ 80%, medium ≥ 50%")),
	}
}
