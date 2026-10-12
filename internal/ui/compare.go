package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/data"
	"tp-inspector/internal/trace"
)

var compareTabs = []string{"Traces", "History"}

type compareView struct {
	a, b    string
	sub     int
	gen     int
	tTraces Table
	tHist   Table
	msgs    map[string][2]string
}

func newCompareView(a, b string) *compareView {
	return &compareView{a: a, b: b, gen: -1}
}

func (v *compareView) Title() string { return v.a + " vs " + v.b }
func (v *compareView) Keys() []string {
	k := []string{keyHint("tab", "section")}
	return append(k, keyHint("a/b", "details of A/B"))
}

func (v *compareView) rebuild(m *Model) {
	sa, sb := m.student(v.a), m.student(v.b)
	var ta, tb *trace.Trace
	if sa != nil {
		ta = sa.Traces[m.sd]
	}
	if sb != nil {
		tb = sb.Traces[m.sd]
	}

	// Traces: tests failed by at least one of them.
	df := map[string]int{}
	for _, s := range collectTests(m) {
		df[s.id] = s.fail
	}
	type row struct {
		id           string
		fa, fb, same bool
	}
	byID := map[string]*row{}
	v.msgs = map[string][2]string{}
	add := func(t *trace.Trace, isA bool) {
		if t == nil {
			return
		}
		for _, x := range t.Tests {
			if x.Passed || x.Skipped {
				continue
			}
			r := byID[x.ID]
			if r == nil {
				r = &row{id: x.ID}
				byID[x.ID] = r
			}
			msgs := v.msgs[x.ID]
			if isA {
				r.fa, msgs[0] = true, x.Message
			} else {
				r.fb, msgs[1] = true, x.Message
			}
			v.msgs[x.ID] = msgs
		}
	}
	add(ta, true)
	add(tb, false)
	var rs []*row
	for _, r := range byID {
		r.same = r.fa && r.fb && v.msgs[r.id][0] == v.msgs[r.id][1]
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool {
		bi, bj := rs[i].fa && rs[i].fb, rs[j].fa && rs[j].fb
		if bi != bj {
			return bi
		}
		if df[rs[i].id] != df[rs[j].id] {
			return df[rs[i].id] < df[rs[j].id]
		}
		return rs[i].id < rs[j].id
	})
	mark := func(failed bool) string {
		if failed {
			return sBad.Render("✗")
		}
		return sGood.Render("✓")
	}
	v.tTraces.Cols = []Column{{Title: "Test", Width: 0}, {Title: "A", Width: 1}, {Title: "B", Width: 1},
		{Title: "Failed by", Width: 9, Right: true}, {Title: "Output", Width: 12}}
	var rows []Row
	for _, r := range rs {
		same := ""
		switch {
		case r.same:
			same = sBad.Bold(true).Render("identical")
		case r.fa && r.fb:
			same = sDim.Render("different")
		}
		cls := r.id
		if i := strings.Index(cls, "::"); i >= 0 {
			c := cls[:i]
			cls = c[strings.LastIndex(c, ".")+1:] + sDim.Render(" › ") + cls[i+2:]
		}
		rows = append(rows, Row{Key: r.id, Cells: []string{cls, mark(r.fa), mark(r.fb), fmt.Sprint(df[r.id]), same}})
	}
	v.tTraces.Empty = "Neither of them fails any test."
	if ta == nil || tb == nil {
		v.tTraces.Empty = "One of them has no trace in " + m.sd + "."
	}
	v.tTraces.SetRows(rows)

	// History: A's tags then B's, each newest first, with the time to the
	// closest submission of the other student.
	closest := func(t time.Time, other *data.Student) (time.Duration, bool) {
		var best time.Duration
		found := false
		if other != nil {
			for _, o := range other.Subs {
				d := t.Sub(o.ReceivedAt)
				if d < 0 {
					d = -d
				}
				if !found || d < best {
					best, found = d, true
				}
			}
		}
		return best, found
	}
	v.tHist.Cols = []Column{{Title: "Who", Width: 24}, {Title: "Tag", Width: 24}, {Title: "Def.", Width: 7},
		{Title: "Date", Width: 11}, {Title: "Grade", Width: 5, Right: true}, {Title: "Closest from other", Width: 18},
		{Title: "IP", Width: 0}}
	rows = nil
	sides := []struct {
		label     string
		login     string
		st, other *data.Student
	}{{sAccent.Render("A "), v.a, sa, sb}, {sWarn.Render("B "), v.b, sb, sa}}
	for i, side := range sides {
		if i > 0 {
			rows = append(rows, Row{Key: "separator"})
		}
		if side.st == nil {
			continue
		}
		for _, sub := range side.st.Subs { // already newest first
			delta := ""
			if d, ok := closest(sub.ReceivedAt, side.other); ok {
				delta = fmtDuration(d)
				if d < 10*time.Minute {
					delta = sBad.Render(delta)
				} else {
					delta = sDim.Render(delta)
				}
			}
			rows = append(rows, Row{Key: sub.ID, Cells: []string{side.label + side.login, sub.Ref,
				sDim.Render(shortSD(sub.SubmissionDefinitionURI)), fmtTime(sub.ReceivedAt), pctCell(sub.Percent()),
				delta, sDim.Render(sub.IP)}})
		}
	}
	v.tHist.SetRows(rows)
}

func (v *compareView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "tab", "right":
		v.sub = (v.sub + 1) % len(compareTabs)
		return nil
	case "shift+tab", "left":
		v.sub = (v.sub + len(compareTabs) - 1) % len(compareTabs)
		return nil
	case "a":
		m.push(newDetailView(v.a))
		return nil
	case "b":
		m.push(newDetailView(v.b))
		return nil
	}
	t := []*Table{&v.tTraces, &v.tHist}[v.sub]
	if t.HandleKey(k.String()) {
		return nil
	}
	return nil
}

func (v *compareView) side(m *Model, login string, w int) string {
	st := m.student(login)
	if st == nil {
		return sDim.Render(login + " (unknown)")
	}
	last := st.Last(m.sd)
	lines := []string{sBold.Render(login) + "  " + pctCell(st.Percent(m.sd))}
	if last != nil {
		lines = append(lines, sDim.Render(fmt.Sprintf("%s · %s · %d submissions", last.Ref, fmtTime(last.ReceivedAt), len(st.SubsFor(m.sd)))))
	} else {
		lines = append(lines, sDim.Render("no submission in "+m.sd))
	}
	return lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
}

func ipsOf(st *data.Student) map[string]bool {
	out := map[string]bool{}
	if st != nil {
		for _, s := range st.Subs {
			if s.IP != "" {
				out[s.IP] = true
			}
		}
	}
	return out
}

func (v *compareView) View(m *Model, w, h int) string {
	if v.gen != m.gen {
		v.rebuild(m)
		v.gen = m.gen
	}
	half := (w - 4) / 2
	head := lipgloss.JoinHorizontal(lipgloss.Top,
		sAccent.Render("A ")+v.side(m, v.a, half-2), "  ", sWarn.Render("B ")+v.side(m, v.b, half-2))

	// Pair-level signals.
	var sig []string
	for _, p := range m.pairsTraces() {
		if p.A == v.a && p.B == v.b || p.A == v.b && p.B == v.a {
			sig = append(sig, "traces "+scoreCell(p.Score))
		}
	}
	ia, ib := ipsOf(m.student(v.a)), ipsOf(m.student(v.b))
	var common []string
	for ip := range ia {
		if ib[ip] {
			common = append(common, ip)
		}
	}
	if len(common) > 0 {
		sort.Strings(common)
		sig = append(sig, sWarn.Render("same IP: "+strings.Join(common, ", ")))
	}
	if len(sig) == 0 {
		sig = append(sig, sDim.Render("no signals"))
	}
	head += "\n" + strings.Join(sig, sDim.Render("  ·  "))

	body := ""
	bodyH := h - 5
	switch v.sub {
	case 0:
		panelH := 4
		body = v.tTraces.View(w, bodyH-panelH)
		if sel := v.tTraces.Selected(); sel != nil {
			ms := v.msgs[sel.Key]
			fmtMsg := func(s string) string {
				if s == "" {
					return sGood.Render("passes")
				}
				return s
			}
			body += "\n" + lipgloss.NewStyle().MaxWidth(w).Render(
				sAccent.Render("A ")+fmtMsg(ms[0])+"\n"+sWarn.Render("B ")+fmtMsg(ms[1]))
		}
	case 1:
		body = v.tHist.View(w, bodyH)
	}
	return head + "\n\n" + renderTabs(compareTabs, v.sub) + "\n" + body
}
