package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tp-inspector/internal/trace"
)

type testStat struct {
	id, class, name string
	pass, fail      int
	gpass, gfail    int               // same, for the group only
	failers         map[string]string // login -> message
	global          bool              // see trace.IsGlobalCheck
}

func (s *testStat) rate() float64 {
	if s.pass+s.fail == 0 {
		return 0
	}
	return float64(s.pass) / float64(s.pass+s.fail)
}

// groupRate is the pass rate of the group, -1 when no member ran the test.
func (s *testStat) groupRate() float64 {
	if s.gpass+s.gfail == 0 {
		return -1
	}
	return float64(s.gpass) / float64(s.gpass+s.gfail)
}

func collectTests(m *Model) []*testStat { return collectTestsFor(m, m.sd) }

// collectTestsFor aggregates the graded traces of one submission definition.
func collectTestsFor(m *Model, sd string) []*testStat {
	stats := map[string]*testStat{}
	g := m.group()
	for login, tr := range m.tracesFor(sd) {
		for _, t := range tr.Tests {
			if t.Skipped {
				continue
			}
			s := stats[t.ID]
			if s == nil {
				s = &testStat{id: t.ID, class: t.Class, name: t.Name, failers: map[string]string{}}
				stats[t.ID] = s
			}
			if trace.IsGlobalCheck(t) {
				s.global = true
			}
			if t.Passed {
				s.pass++
				if g[login] {
					s.gpass++
				}
			} else {
				s.fail++
				s.failers[login] = t.Message
				if g[login] {
					s.gfail++
				}
			}
		}
	}
	out := make([]*testStat, 0, len(stats))
	for _, s := range stats {
		out = append(out, s)
	}
	return out
}

var testsSorts = []string{"pass rate", "name", "group gap"}

type testsView struct {
	t        Table
	sortMode int // index in testsSorts; the gap is only offered with a group
	group    bool
	gen      int
	stats    map[string]*testStat
	hidden   int // global checks not shown
}

func (v *testsView) Title() string { return "Tests" }
func (v *testsView) Keys() []string {
	return []string{keyHint("s", "sort: "+testsSorts[v.sortMode]), keyHint("enter", "who fails")}
}

func (v *testsView) rebuild(m *Model) {
	// Memory-error counters, build checks and compile failures are global to
	// an exercise: they are shown when comparing two students, not here.
	var stats []*testStat
	v.hidden = 0
	for _, s := range collectTests(m) {
		if s.global {
			v.hidden++
			continue
		}
		stats = append(stats, s)
	}
	v.stats = map[string]*testStat{}
	v.group = m.hasGroup()
	if !v.group && v.sortMode == 2 {
		v.sortMode = 0
	}
	// gap is how far the group is below the class; tests no member ran last.
	gap := func(s *testStat) float64 {
		if s.groupRate() < 0 {
			return 2
		}
		return s.groupRate() - s.rate()
	}
	sort.Slice(stats, func(i, j int) bool {
		switch v.sortMode {
		case 0:
			if stats[i].rate() != stats[j].rate() {
				return stats[i].rate() < stats[j].rate()
			}
		case 2:
			if gi, gj := gap(stats[i]), gap(stats[j]); gi != gj {
				return gi < gj
			}
		}
		return stats[i].id < stats[j].id
	})
	v.t.Cols = []Column{
		{Title: "Category", Width: 28},
		{Title: "Test", Width: 0},
		{Title: "Pass", Width: 5, Right: true},
		{Title: "", Width: 14},
	}
	if v.group {
		v.t.Cols = append(v.t.Cols, Column{Title: "Group", Width: 5, Right: true}, Column{Title: "Gap", Width: 10, Right: true})
	}
	v.t.Cols = append(v.t.Cols,
		Column{Title: "Fail", Width: 6, Right: true},
		Column{Title: "Distinct outputs", Width: 17, Right: true})
	rows := make([]Row, len(stats))
	for i, s := range stats {
		v.stats[s.id] = s
		msgs := map[string]bool{}
		for _, msg := range s.failers {
			msgs[msg] = true
		}
		p := int(s.rate() * 100)
		cls := s.class[strings.LastIndex(s.class, ".")+1:]
		cells := []string{sDim.Render(trace.Category(s.class)), cls + sDim.Render(" › ") + s.name, pctCell(p), pctBar(p, 14)}
		if v.group {
			if gr := s.groupRate(); gr >= 0 {
				gp := int(gr * 100)
				cells = append(cells, pctCell(gp), deltaCell(float64(gp-p), " pts"))
			} else {
				cells = append(cells, sDim.Render("—"), "")
			}
		}
		rows[i] = Row{Key: s.id, Cells: append(cells, fmt.Sprint(s.fail), sDim.Render(fmt.Sprint(len(msgs))))}
	}
	v.t.Empty = "No traces for this submission definition."
	v.t.SetRows(rows)
}

func (v *testsView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.t.HandleKey(k.String()) {
		return nil
	}
	switch k.String() {
	case "s":
		n := len(testsSorts)
		if !v.group {
			n-- // no gap without a group
		}
		v.sortMode = (v.sortMode + 1) % n
		v.gen = -1
	case "enter":
		if sel := v.t.Selected(); sel != nil {
			if s := v.stats[sel.Key]; s != nil && s.fail > 0 {
				m.push(newTestDetailView(s, m.group()))
			}
		}
	}
	return nil
}

func (v *testsView) View(m *Model, w, h int) string {
	if v.gen != m.gen || v.stats == nil {
		v.rebuild(m)
		v.gen = m.gen
	}
	if v.hidden == 0 {
		return v.t.View(w, h)
	}
	note := sDim.Render(fmt.Sprintf("%d global checks hidden (memory errors, build errors, \"Does not compile\"), it is visible when comparing two students", v.hidden))
	return note + "\n" + v.t.View(w, h-1)
}

// testDetailView groups the students failing a test by their exact output:
// a rare wrong output shared by a few students is a strong hint.
type testDetailView struct {
	s      *testStat
	t      Table
	groups map[string][]string
	mine   map[string]bool // the group (--logins + marked)
}

func newTestDetailView(s *testStat, mine map[string]bool) *testDetailView {
	v := &testDetailView{s: s, groups: map[string][]string{}, mine: mine}
	for login, msg := range s.failers {
		v.groups[msg] = append(v.groups[msg], login)
	}
	type g struct {
		msg    string
		logins []string
	}
	var gs []g
	for msg, ls := range v.groups {
		sort.Strings(ls)
		gs = append(gs, g{msg, ls})
	}
	// Small shared groups first: those are the interesting ones.
	sort.Slice(gs, func(i, j int) bool {
		ri, rj := len(gs[i].logins), len(gs[j].logins)
		if (ri >= 2) != (rj >= 2) {
			return ri >= 2
		}
		if ri != rj {
			return ri < rj
		}
		return gs[i].msg < gs[j].msg
	})
	v.t.Cols = []Column{{Title: "Students", Width: 8, Right: true}}
	if len(mine) > 0 {
		v.t.Cols = append(v.t.Cols, Column{Title: "In group", Width: 8, Right: true})
	}
	v.t.Cols = append(v.t.Cols, Column{Title: "Output", Width: 0})
	for _, x := range gs {
		msg := x.msg
		if msg == "" {
			msg = sDim.Render("(no message)")
		}
		n := fmt.Sprint(len(x.logins))
		if len(x.logins) >= 2 && len(x.logins) <= 5 {
			n = sWarn.Bold(true).Render(n)
		}
		cells := []string{n}
		if len(mine) > 0 {
			k := 0
			for _, l := range x.logins {
				if mine[l] {
					k++
				}
			}
			in := sDim.Render("·")
			if k > 0 {
				in = sAccent.Render(fmt.Sprint(k))
			}
			cells = append(cells, in)
		}
		v.t.Rows = append(v.t.Rows, Row{Key: x.msg, Cells: append(cells, msg)})
	}
	return v
}

func (v *testDetailView) Title() string { return v.s.name }
func (v *testDetailView) Keys() []string {
	return []string{keyHint("enter", "compare (if 2)")}
}

func (v *testDetailView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.t.HandleKey(k.String()) {
		return nil
	}
	if k.String() == "enter" {
		if sel := v.t.Selected(); sel != nil {
			if ls := v.groups[sel.Key]; len(ls) == 2 {
				m.push(newCompareView(ls[0], ls[1]))
			} else if len(ls) == 1 {
				m.push(newDetailView(ls[0]))
			} else {
				return m.setStatus("Mark two of them in Ranking to compare them")
			}
		}
	}
	return nil
}

func (v *testDetailView) View(m *Model, w, h int) string {
	head := sBold.Render(v.s.id) + "\n" +
		sDim.Render(fmt.Sprintf("%d pass · %d fail · %d distinct outputs", v.s.pass, v.s.fail, len(v.groups))) + "\n\n"
	panelH := 5
	tableH := max(3, h-3-panelH)
	panel := ""
	if sel := v.t.Selected(); sel != nil {
		var ls []string
		for _, l := range v.groups[sel.Key] {
			if v.mine[l] {
				l = "◦" + l
			}
			ls = append(ls, l)
		}
		hdr := "Students with this output"
		if len(v.mine) > 0 {
			hdr += sDim.Render("  (◦ = your group)")
		}
		panel = sSectionHdr.Render(hdr) + "\n" + wrapWords(strings.Join(ls, "  "), w, panelH-1)
	}
	return head + v.t.View(w, tableH) + "\n" + panel
}

// wrapWords wraps text to width w, keeping at most maxLines lines.
func wrapWords(s string, w, maxLines int) string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		if cur != "" && len(cur)+2+len(word) > w {
			lines = append(lines, cur)
			cur = ""
		}
		if cur != "" {
			cur += "  "
		}
		cur += word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], sDim.Render("… and more"))
	}
	return strings.Join(lines, "\n")
}
