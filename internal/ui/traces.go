package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type tracesView struct {
	t   Table
	gen int
}

func (v *tracesView) Title() string  { return "Traces" }
func (v *tracesView) Keys() []string { return []string{keyHint("enter", "compare")} }

func (v *tracesView) rebuild(m *Model) {
	v.t.Cols = []Column{
		{Title: "#", Width: 4, Right: true},
		{Title: "Student A", Width: 0},
		{Title: "Student B", Width: 0},
		{Title: "Score", Width: 5, Right: true},
		{Title: "", Width: 10},
		{Title: "Shared fails", Width: 14, Right: true},
		{Title: "Same output", Width: 12, Right: true},
		{Title: "Fails A/B", Width: 10, Right: true},
		{Title: "Grades", Width: 9, Right: true},
	}
	pairs := m.pairsTraces()
	rows := make([]Row, len(pairs))
	for i, p := range pairs {
		pa, pb := -1, -1
		if st := m.student(p.A); st != nil {
			pa = st.Percent(m.sd)
		}
		if st := m.student(p.B); st != nil {
			pb = st.Percent(m.sd)
		}
		rows[i] = Row{Key: p.A + "\x00" + p.B, Cells: []string{
			sDim.Render(fmt.Sprint(i + 1)), p.A, p.B, scoreCell(p.Score), bar(p.Score, 10, sAccent),
			fmt.Sprint(len(p.Shared)), fmt.Sprint(p.SameMsg), fmt.Sprintf("%d/%d", p.FailA, p.FailB),
			pctCell(pa) + sDim.Render("/") + pctCell(pb),
		}}
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
			pairs := m.pairsTraces()
			p := pairs[v.t.Cursor()]
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
	head := sDim.Render("Pairs failing the same uncommon tests. A test almost nobody fails weighs more, and even more with an identical output.") + "\n\n"
	return head + v.t.View(w, h-2)
}
