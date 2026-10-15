package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/analysis"
)

// codeView lists the pairs of repos sharing code.
type codeView struct {
	t         Table
	gen       int
	onlyGroup bool // show only pairs involving the group (when there is one)
	filtered  bool // onlyGroup applied at the last rebuild
	pairs     map[string]analysis.CodePair
}

func (v *codeView) Title() string { return "Code" }
func (v *codeView) Keys() []string {
	return []string{keyHint("enter", "compare"), keyHint("f", "group / everyone")}
}

func (v *codeView) rebuild(m *Model) {
	v.t.Cols = []Column{
		{Title: "#", Width: 4, Right: true},
		{Title: "Level", Width: 6},
		{Title: "Student A", Width: 24},
		{Title: "Student B", Width: 24},
		{Title: "Why", Width: 0},
		{Title: "Score", Width: 5, Right: true},
		{Title: "AI A/B", Width: 13},
		{Title: "Grades", Width: 9, Right: true},
	}
	g := m.group()
	v.filtered = v.onlyGroup && len(g) > 0
	name := func(l string) string {
		if g[l] {
			return sAccent.Render("◦ ") + l
		}
		return "  " + l
	}
	ai := func(l string) string {
		if s, ok := m.aiOf(l); ok {
			return levelCell(s.Score)
		}
		return sDim.Render("·")
	}
	v.pairs = map[string]analysis.CodePair{}
	var rows []Row
	for _, p := range m.codePairs() {
		if v.filtered && !pairInvolves(p.A, p.B, g) {
			continue
		}
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
			sDim.Render(fmt.Sprint(len(rows) + 1)), levelCell(p.Score), name(p.A), name(p.B), p.Reason(),
			sDim.Render(scorePct(p.Score)), ai(p.A) + sDim.Render("/") + ai(p.B), pctCell(pa) + sDim.Render("/") + pctCell(pb),
		}})
	}
	switch {
	case m.analyzing:
		v.t.Empty = fmt.Sprintf("Analysing the repos… %d/%d", m.anProg.done, m.anProg.total)
	case m.code == nil:
		v.t.Empty = "No repo analysed: press A in the Repos tab to clone " + m.classLabel() + " (or a for the marked students)."
	case v.filtered:
		v.t.Empty = "No pair sharing code involves your group (f to show everyone)."
	default:
		v.t.Empty = fmt.Sprintf("No pair shares code among the %d repos on disk.", len(m.code.Repos))
	}
	v.t.SetRows(rows)
}

func (v *codeView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.t.HandleKey(k.String()) {
		return nil
	}
	switch k.String() {
	case "f":
		v.onlyGroup = !v.onlyGroup
		v.gen = -1
	case "enter":
		if sel := v.t.Selected(); sel != nil {
			p := v.pairs[sel.Key]
			m.push(newCompareView(p.A, p.B))
		}
	}
	return nil
}

func (v *codeView) View(m *Model, w, h int) string {
	if v.gen != m.gen || m.analyzing {
		v.rebuild(m)
		v.gen = m.gen
	}
	head := sDim.Render("Pairs of repos with function bodies alike once names and layout are ignored; code most of the promo shares (the skeleton) does not count.")
	if m.code != nil {
		head += "\n" + sDim.Render(fmt.Sprintf("%d repos compared (only the repos on disk: A in Repos clones %s).", len(m.code.Repos), m.classLabel()))
	}
	if v.filtered {
		head += "\n" + sAccent.Render(fmt.Sprintf("Only pairs with a member of your group (◦): %d", len(v.t.Rows))) + sDim.Render(" · f to show everyone")
	} else if m.hasGroup() {
		head += "\n" + sDim.Render("Everyone · f to show only pairs with your group (◦)")
	}
	head += "\n\n"
	why := ""
	if sel := v.t.Selected(); sel != nil {
		p := v.pairs[sel.Key]
		why = "\n" + sSectionHdr.Render("Why "+p.A+" & "+p.B) + "\n" +
			lipgloss.NewStyle().MaxWidth(w).Render(strings.Join(codeWhyLines(p), "\n"))
	}
	used := strings.Count(head, "\n") + strings.Count(why, "\n")
	return head + v.t.View(w, max(3, h-used)) + why
}
