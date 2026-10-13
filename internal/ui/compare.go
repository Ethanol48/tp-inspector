package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/analysis"
	"tp-inspector/internal/data"
)

var compareTabs = []string{"Code", "History"}

type compareView struct {
	a, b    string
	sub     int
	gen     int
	tCode   Table
	tHist   Table
	pair    *analysis.CodePair // nil when they share no code
	matches map[string]analysis.FuncMatch
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

	// Code: their matched functions, best first.
	v.pair, v.matches = nil, map[string]analysis.FuncMatch{}
	for _, p := range m.codePairs() {
		if p.A == v.a && p.B == v.b || p.A == v.b && p.B == v.a {
			p := p
			v.pair = &p
		}
	}
	v.tCode.Cols = []Column{{Title: "Function of A", Width: 48}, {Title: "Function of B", Width: 0},
		{Title: "Alike", Width: 5, Right: true}, {Title: "", Width: 9}, {Title: "Tokens", Width: 6, Right: true}}
	var rows []Row
	if v.pair != nil {
		for i, mt := range v.pair.Matches {
			fa, fb := mt.A, mt.B
			if v.pair.A != v.a { // the pair is stored in login order
				fa, fb = fb, fa
			}
			key := fmt.Sprint(i)
			v.matches[key] = analysis.FuncMatch{A: fa, B: fb, Sim: mt.Sim, Tokens: mt.Tokens, Identical: mt.Identical}
			loc := func(f *analysis.Func) string {
				return f.Name + sDim.Render(fmt.Sprintf("  %s:%d", f.File, f.Start))
			}
			same := sDim.Render("renamed")
			if mt.Identical {
				same = sBad.Bold(true).Render("identical")
			}
			rows = append(rows, Row{Key: key, Cells: []string{loc(fa), loc(fb), scorePct(mt.Sim), same, sDim.Render(fmt.Sprint(mt.Tokens))}})
		}
	}
	switch {
	case m.code == nil:
		v.tCode.Empty = "Repos not analysed: clone them in the Repos tab (A for " + m.classLabel() + ")."
	case m.code.Repos[v.a] == nil || m.code.Repos[v.b] == nil:
		v.tCode.Empty = "One of them has no repo on disk: clone it in the Repos tab."
	default:
		v.tCode.Empty = "No function alike between their repos."
	}
	v.tCode.SetRows(rows)

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
	t := []*Table{&v.tCode, &v.tHist}[v.sub]
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
	why := ""
	if v.pair != nil {
		sig = append(sig, "code: "+levelCell(v.pair.Score)+sDim.Render(" — ")+v.pair.Reason())
		why = "\n" + lipgloss.NewStyle().MaxWidth(w).Render(strings.Join(codeWhyLines(*v.pair), "\n"))
	}
	for _, side := range []struct{ tag, login string }{{"A", v.a}, {"B", v.b}} {
		var parts []string
		if r := m.riskScores()[side.login]; r.Score > 0 {
			parts = append(parts, "risk "+levelCell(r.Score))
		}
		if s, ok := m.aiOf(side.login); ok {
			parts = append(parts, "AI "+levelCell(s.Score))
		}
		if len(parts) > 0 {
			sig = append(sig, side.tag+": "+strings.Join(parts, " "))
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
	head += "\n" + strings.Join(sig, sDim.Render("  ·  ")) + why

	body := ""
	bodyH := h - 5 - strings.Count(why, "\n")
	switch v.sub {
	case 0:
		listH := min(len(v.tCode.Rows)+2, max(4, bodyH/3))
		body = v.tCode.View(w, listH)
		if sel := v.tCode.Selected(); sel != nil {
			mt := v.matches[sel.Key]
			codeH := bodyH - listH - 2
			half := (w - 3) / 2
			pane := func(label string, style lipgloss.Style, f *analysis.Func) string {
				lines := strings.Split(strings.ReplaceAll(f.Raw, "\t", "    "), "\n")
				if len(lines) > codeH {
					lines = append(lines[:max(0, codeH-1)], sDim.Render(fmt.Sprintf("… %d more lines", len(lines)-codeH+1)))
				}
				title := style.Render(label+" ") + sBold.Render(f.Name) + sDim.Render(fmt.Sprintf(" %s:%d-%d", f.File, f.Start, f.End))
				return lipgloss.NewStyle().Width(half).MaxWidth(half).Render(title + "\n" + strings.Join(lines, "\n"))
			}
			if codeH > 2 {
				body += "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, pane("A", sAccent, mt.A), sDim.Render(" │ "), pane("B", sWarn, mt.B))
			}
		}
	case 1:
		body = v.tHist.View(w, bodyH)
	}
	return head + "\n\n" + renderTabs(compareTabs, v.sub) + "\n" + body
}
