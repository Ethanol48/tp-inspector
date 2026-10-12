package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/analysis"
)

var groupSections = []string{"Members", "Exercises"}

// groupView compares the group (--logins + marked students) with the class.
type groupView struct {
	sub         int
	gen         int
	tMem, tEx   Table
	class, grp  gradeStats
	avgPctl     int
	suspectPair int // code pairs with level ≥ medium involving the group
}

func (v *groupView) Title() string { return "Group" }
func (v *groupView) Keys() []string {
	k := []string{keyHint("t", "section: "+groupSections[(v.sub+1)%len(groupSections)])}
	if v.sub == 0 {
		k = append(k, keyHint("enter", "student"), keyHint("H", "history"))
	}
	return k
}

func (v *groupView) rebuild(m *Model) {
	g := m.group()
	v.class = computeStats(m.ds.Students, m.sd)
	members := m.groupStudents()
	v.grp = computeStats(members, m.sd)
	q1 := quartile(v.class.percents)

	// Members: problems first, then by grade.
	type member struct {
		login string
		pct   int
		flags []string
		score float64
	}
	var ms []member
	sumPctl, nPctl := 0, 0
	for _, st := range members {
		sus, known := m.suspicionOf(st.Login)
		e := member{login: st.Login, pct: st.Percent(m.sd), score: sus}
		last := st.Last(m.sd)
		switch {
		case last == nil:
			e.flags = append(e.flags, sBad.Render("no submission"))
		case last.Status == "ERROR":
			e.flags = append(e.flags, sBad.Render("errored"))
		}
		if e.pct >= 0 {
			sumPctl += percentile(e.pct, v.class.percents)
			nPctl++
			if e.pct < q1 {
				e.flags = append(e.flags, sWarn.Render("bottom 25%"))
			}
		}
		if known && analysis.LevelOf(e.score) >= analysis.Medium {
			e.flags = append(e.flags, sWarn.Render("suspect"))
		}
		ms = append(ms, e)
	}
	v.avgPctl = -1
	if nPctl > 0 {
		v.avgPctl = int(math.Round(float64(sumPctl) / float64(nPctl)))
	}
	sort.SliceStable(ms, func(i, j int) bool {
		if (len(ms[i].flags) > 0) != (len(ms[j].flags) > 0) {
			return len(ms[i].flags) > 0
		}
		if ms[i].pct != ms[j].pct {
			return ms[i].pct < ms[j].pct
		}
		return ms[i].login < ms[j].login
	})
	v.tMem.Cols = []Column{{Title: "Login", Width: 0}, {Title: "Grade", Width: 5, Right: true}, {Title: "", Width: 12},
		{Title: "Class pctl", Width: 10, Right: true}, {Title: "Status", Width: 13}, {Title: "Suspicion", Width: 9},
		{Title: "Flags", Width: 28}}
	rows := make([]Row, len(ms))
	for i, e := range ms {
		pctl := sDim.Render("—")
		if e.pct >= 0 {
			pctl = fmt.Sprintf("%d", percentile(e.pct, v.class.percents))
		}
		var si *statusInfo
		if last := m.student(e.login).Last(m.sd); last != nil {
			si = &statusInfo{last.Status, last.Error}
		}
		rows[i] = Row{Key: e.login, Cells: []string{e.login, pctCell(e.pct), pctBar(e.pct, 12), pctl,
			statusCell(si), suspicionCell(m, e.login), strings.Join(e.flags, sDim.Render(", "))}}
	}
	v.tMem.Empty = "No group: pass --logins list.txt or mark students with space in Ranking."
	v.tMem.SetRows(rows)

	// Exercises: worst gap first.
	cls, grp := m.categoryRates(m.sd, nil), m.categoryRates(m.sd, g)
	type ex struct {
		cat    string
		gp, cp int
	}
	var es []ex
	for c, r := range cls {
		gr, ok := grp[c]
		if !ok {
			continue
		}
		es = append(es, ex{c, gr.pct(), r.pct()})
	}
	sort.Slice(es, func(i, j int) bool {
		if di, dj := es[i].gp-es[i].cp, es[j].gp-es[j].cp; di != dj {
			return di < dj
		}
		return es[i].cat < es[j].cat
	})
	v.tEx.Cols = []Column{{Title: "Exercise", Width: 0}, {Title: "Group", Width: 5, Right: true}, {Title: "", Width: 14},
		{Title: "Class", Width: 5, Right: true}, {Title: "Gap", Width: 10, Right: true}}
	rows = make([]Row, len(es))
	for i, e := range es {
		rows[i] = Row{Key: e.cat, Cells: []string{e.cat, pctCell(e.gp), pctBar(e.gp, 14), pctCell(e.cp),
			deltaCell(float64(e.gp-e.cp), " pts")}}
	}
	v.tEx.Empty = "No traces for the group in " + m.sd + "."
	v.tEx.SetRows(rows)

	v.suspectPair = 0
	for _, p := range m.codePairs() {
		if pairInvolves(p.A, p.B, g) && p.Level() >= analysis.Medium {
			v.suspectPair++
		}
	}
}

func (v *groupView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if k.String() == "t" {
		v.sub = (v.sub + 1) % len(groupSections)
		return nil
	}
	t := []*Table{&v.tMem, &v.tEx}[v.sub]
	if t.HandleKey(k.String()) {
		return nil
	}
	if v.sub == 0 {
		if sel := v.tMem.Selected(); sel != nil {
			switch k.String() {
			case "enter":
				m.push(newDetailView(sel.Key))
			case "H":
				m.openHistory(sel.Key)
			}
		}
	}
	return nil
}

// statsTable renders the Group | Class | Δ comparison.
func (v *groupView) statsTable() string {
	g, c := v.grp, v.class
	line := func(label, gv, cv, d string) string {
		return sDim.Render(padRight(label, 15)) + padLeft(gv, 8) + padLeft(cv, 8) + "   " + d
	}
	pctOf := func(n, total int) string {
		if total == 0 {
			return ""
		}
		return sDim.Render(fmt.Sprintf(" (%d%%)", int(math.Round(100*float64(n)/float64(total)))))
	}
	pcts := func(f float64, n int) string {
		if n == 0 {
			return sDim.Render("—")
		}
		return pctStyle(int(f)).Render(fmt.Sprintf("%.1f%%", f))
	}
	lines := []string{
		sHeader.Render(padRight("", 15)) + padLeft(sHeader.Render("Group"), 8) + padLeft(sHeader.Render("Class"), 8),
		line("students", fmt.Sprint(g.n), fmt.Sprint(c.n), ""),
		line("submitted", fmt.Sprint(g.delivered), fmt.Sprint(c.delivered), pctOf(g.delivered, g.n)),
		line("no submission", fmt.Sprint(g.missing), fmt.Sprint(c.missing), pctOf(g.missing, g.n)),
		line("errored", fmt.Sprint(g.errors), fmt.Sprint(c.errors), ""),
		line("mean", pcts(g.mean, len(g.percents)), pcts(c.mean, len(c.percents)), deltaCell(g.mean-c.mean, " pts")),
		line("median", pcts(g.median, len(g.percents)), pcts(c.median, len(c.percents)), deltaCell(g.median-c.median, " pts")),
		line("at 100%", fmt.Sprint(g.full), fmt.Sprint(c.full), ""),
	}
	if v.avgPctl >= 0 {
		lines = append(lines, sDim.Render(padRight("class pctl", 15))+padLeft(fmt.Sprint(v.avgPctl), 8)+
			sDim.Render(fmt.Sprintf("   on average the group beats %d%% of the class (50 = average)", v.avgPctl)))
	}
	sus := sDim.Render("none")
	if v.suspectPair > 0 {
		sus = sWarn.Render(fmt.Sprint(v.suspectPair))
	}
	lines = append(lines, sDim.Render(padRight("code pairs", 15))+padLeft(sus, 8)+sDim.Render("   sharing code (medium or high) with a group member (Code tab)"))
	return strings.Join(lines, "\n")
}

func (v *groupView) View(m *Model, w, h int) string {
	if v.gen != m.gen {
		v.rebuild(m)
		v.gen = m.gen
	}
	if v.grp.n == 0 {
		return sDim.Render("No group yet.\n\nPass a list of logins with --logins list.txt, or mark students with space in the Ranking tab:\n" +
			"this tab then compares them with the whole class.")
	}
	head := v.statsTable() + "\n\n" + renderTabs(groupSections, v.sub) + "\n"
	used := strings.Count(head, "\n") + 1
	t := []*Table{&v.tMem, &v.tEx}[v.sub]
	return head + t.View(w, max(3, h-used))
}

func padLeft(s string, n int) string {
	return strings.Repeat(" ", max(0, n-lipgloss.Width(s))) + s
}
