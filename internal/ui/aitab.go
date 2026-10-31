package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Levels of the AI score, as in analysis.LevelOf: high from 80 %, medium from 50 %.
const (
	aiMedium = 0.5
	aiHigh   = 0.8
)

// aiTabView ranks the analysed repos by how much they look AI-generated.
type aiTabView struct {
	t         Table
	gen       int
	onlyGroup bool // show only the group (when there is one)
	filtered  bool // onlyGroup applied at the last rebuild
	onlyFlag  bool // hide the repos below the medium level
}

func (v *aiTabView) Title() string { return "AI" }

func (v *aiTabView) Keys() []string {
	return []string{keyHint("enter", "signals"), keyHint("f", "group / everyone"), keyHint("h", "flagged / all")}
}

func (v *aiTabView) rebuild(m *Model) {
	v.t.Cols = []Column{
		{Title: "#", Width: 4, Right: true},
		{Title: "Level", Width: 6},
		{Title: "Student", Width: 24},
		{Title: "Score", Width: 5, Right: true},
		{Title: "Tool", Width: 14},
		{Title: "Why", Width: 0},
		{Title: "Commits", Width: 7, Right: true},
		{Title: "Grade", Width: 5, Right: true},
	}
	g := m.group()
	v.filtered = v.onlyGroup && len(g) > 0
	name := func(l string) string {
		if g[l] {
			return sAccent.Render("◦ ") + l
		}
		return "  " + l
	}

	var logins []string
	if m.code != nil {
		for l := range m.code.AI {
			logins = append(logins, l)
		}
	}
	sort.Slice(logins, func(i, j int) bool {
		a, b := m.code.AI[logins[i]].Score, m.code.AI[logins[j]].Score
		if a != b {
			return a > b
		}
		return logins[i] < logins[j]
	})

	var rows []Row
	for _, l := range logins {
		s := m.code.AI[l]
		if v.filtered && !g[l] {
			continue
		}
		if v.onlyFlag && s.Score < aiMedium {
			continue
		}
		grade := -1
		if st := m.student(l); st != nil {
			grade = st.Percent(m.sd)
		}
		tool := sDim.Render("·")
		if s.Agent != "" {
			tool = sBad.Render(s.Agent)
		}
		commits := sDim.Render("·")
		if s.Commits > 0 {
			commits = fmt.Sprint(s.Commits)
		}
		rows = append(rows, Row{Key: l, Cells: []string{
			sDim.Render(fmt.Sprint(len(rows) + 1)), levelCell(s.Score), name(l), sDim.Render(scorePct(s.Score)),
			tool, s.Reason(), commits, pctCell(grade),
		}})
	}

	switch {
	case m.analyzing:
		v.t.Empty = fmt.Sprintf("Analysing the repos… %d/%d", m.anProg.done, m.anProg.total)
	case m.code == nil:
		v.t.Empty = "No repo analysed: press A in the Repos tab to clone " + m.classLabel() + " (or a for the marked students)."
	case v.filtered:
		v.t.Empty = "No analysed repo belongs to your group (f to show everyone)."
	case v.onlyFlag:
		v.t.Empty = "No repo reaches the medium level (h to show every repo)."
	default:
		v.t.Empty = "No repo analysed."
	}
	v.t.SetRows(rows)
}

func (v *aiTabView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.t.HandleKey(k.String()) {
		return nil
	}
	switch k.String() {
	case "f":
		v.onlyGroup = !v.onlyGroup
		v.gen = -1
	case "h":
		v.onlyFlag = !v.onlyFlag
		v.gen = -1
	case "enter":
		if sel := v.t.Selected(); sel != nil {
			m.push(&aiDetailView{login: sel.Key})
		}
	}
	return nil
}

func (v *aiTabView) View(m *Model, w, h int) string {
	if v.gen != m.gen || m.analyzing {
		v.rebuild(m)
		v.gen = m.gen
	}
	head := sDim.Render("Repos ranked by how much they look AI-generated: style far from the promo, typography, chatbot phrasing, generated commit messages, credited tools. Hints, not proof.")
	if m.code != nil {
		high, med := 0, 0
		for _, s := range m.code.AI {
			switch {
			case s.Score >= aiHigh:
				high++
			case s.Score >= aiMedium:
				med++
			}
		}
		head += "\n" + sDim.Render(fmt.Sprintf("%d repos analysed (only the repos on disk) · %d high · %d medium", len(m.code.Repos), high, med))
	}
	switch {
	case v.filtered:
		head += "\n" + sAccent.Render(fmt.Sprintf("Only your group (◦): %d", len(v.t.Rows))) + sDim.Render(" · f to show everyone")
	case m.hasGroup():
		head += "\n" + sDim.Render("Everyone · f to show only your group (◦)")
	}
	if v.onlyFlag {
		head += "\n" + sAccent.Render("Medium and high only") + sDim.Render(" · h to show every repo")
	}
	head += "\n\n"

	why := ""
	if sel := v.t.Selected(); sel != nil {
		if s, ok := m.aiOf(sel.Key); ok && len(s.Signals) > 0 {
			var ls []string
			for i, sg := range s.Signals {
				if i == 4 {
					break
				}
				ls = append(ls, "• "+sg.Detail)
			}
			why = "\n" + sSectionHdr.Render("Why "+sel.Key) + "\n" +
				lipgloss.NewStyle().MaxWidth(w).Render(strings.Join(ls, "\n"))
		}
	}
	used := strings.Count(head, "\n") + strings.Count(why, "\n")
	return head + v.t.View(w, max(3, h-used)) + why
}

// aiDetailView shows every signal of a repo and the functions to read first.
type aiDetailView struct {
	login  string
	offset int
}

func (v *aiDetailView) Title() string { return "AI · " + v.login }

func (v *aiDetailView) Keys() []string { return []string{keyHint("↑/↓", "scroll")} }

func (v *aiDetailView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "up":
		v.offset--
	case "down":
		v.offset++
	case "pgup":
		v.offset -= 10
	case "pgdown":
		v.offset += 10
	case "g", "home":
		v.offset = 0
	case "G", "end":
		v.offset = 1 << 20 // clamped when drawn
	}
	v.offset = max(0, v.offset)
	return nil
}

// aiWrap wraps s to the width w and indents every line.
func aiWrap(s string, w, indent int) []string {
	pad := strings.Repeat(" ", indent)
	var out []string
	for _, l := range strings.Split(lipgloss.NewStyle().Width(max(10, w-indent)).Render(s), "\n") {
		out = append(out, pad+strings.TrimRight(l, " "))
	}
	return out
}

func (v *aiDetailView) View(m *Model, w, h int) string {
	s, ok := m.aiOf(v.login)
	if !ok {
		return sDim.Render("No AI analysis for " + v.login + ": clone and analyse the repo first.")
	}

	lines := []string{
		sBold.Render(v.login) + "  " + levelCell(s.Score) + " " + sDim.Render(scorePct(s.Score)),
		sDim.Render(fmt.Sprintf("style %s · tells %s · %d commits", scorePct(s.Style), scorePct(s.Tells), s.Commits)),
	}
	if s.Agent != "" {
		lines = append(lines, sBad.Render("Credited AI tool: ")+s.Agent)
	}

	lines = append(lines, "", sSectionHdr.Render("Signals"))
	if len(s.Signals) == 0 {
		lines = append(lines, sDim.Render("  nothing unusual"))
	}
	for _, sg := range s.Signals {
		z := ""
		if sg.Z > 0 {
			z = sDim.Render(fmt.Sprintf("  %.1f σ above the promo", sg.Z))
		}
		lines = append(lines, "  "+sBold.Render(sg.Name)+z)
		lines = append(lines, aiWrap(sg.Detail, w, 4)...)
	}

	lines = append(lines, "", sSectionHdr.Render("Functions to read first"))
	if len(s.TopFuncs) == 0 {
		lines = append(lines, sDim.Render("  none stands out"))
	}
	for _, ff := range s.TopFuncs {
		name := "?"
		if ff.Func != nil {
			name = ff.Func.Name
		}
		lines = append(lines, "  "+sBold.Render(name)+sDim.Render(fmt.Sprintf("  %.1f · %s", ff.Score, strings.Join(ff.Why, ", "))))
		if ff.Func == nil {
			continue
		}
		raw := strings.Split(strings.ReplaceAll(strings.TrimRight(ff.Func.Raw, "\r\n"), "\t", "    "), "\n")
		for i, l := range raw {
			if i == 12 {
				lines = append(lines, sDim.Render("    …"))
				break
			}
			lines = append(lines, sDim.Render("    "+strings.TrimRight(l, "\r")))
		}
	}

	v.offset = min(v.offset, max(0, len(lines)-h))
	return strings.Join(lines[v.offset:min(len(lines), v.offset+h)], "\n")
}
