package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/analysis"
)

type overviewView struct{}

func (v *overviewView) Title() string                         { return "Overview" }
func (v *overviewView) Keys() []string                        { return nil }
func (v *overviewView) Update(m *Model, k tea.KeyMsg) tea.Cmd { return nil }

func card(value, label string, style lipgloss.Style) string {
	return sCard.Render(style.Bold(true).Render(value) + "\n" + sDim.Render(label))
}

func (v *overviewView) View(m *Model, w, h int) string {
	ds := m.ds
	cs := computeStats(ds.Students, m.sd)
	percents := cs.percents
	var nSubs int
	days := map[string]int{}
	var firstDay, lastDay time.Time
	for _, st := range ds.Students {
		subs := st.SubsFor(m.sd)
		nSubs += len(subs)
		for _, s := range subs {
			d := s.ReceivedAt.Local()
			day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
			days[day.Format("2006-01-02")]++
			if firstDay.IsZero() || day.Before(firstDay) {
				firstDay = day
			}
			if day.After(lastDay) {
				lastDay = day
			}
		}
	}

	var b strings.Builder
	b.WriteString(sDim.Render(m.slug.Describe()+" · "+ds.Activity.URI) + "\n")
	if win := ds.Window(m.sd); win != nil {
		b.WriteString(sDim.Render(fmt.Sprintf("%s open %s → close %s (%s)", m.sd, fmtTime(win.OpeningAt), fmtTime(win.ClosingAt),
			fmtDuration(win.ClosingAt.Sub(win.OpeningAt)))) + "\n")
	} else {
		b.WriteString(sDim.Render("no submission window from the API for "+m.sd+": timing uses the class's first and last submissions") + "\n")
	}
	b.WriteString("\n")

	// writeCards wraps cards to the available width.
	writeCards := func(cards []string) {
		var row []string
		for _, c := range cards {
			if lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, append(row, c)...)) > w && len(row) > 0 {
				b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, row...) + "\n")
				row = nil
			}
			row = append(row, c)
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, row...) + "\n")
	}
	writeCards([]string{
		card(fmt.Sprint(cs.n), "students", sBold),
		card(fmt.Sprint(cs.delivered), "submitted", sGood),
		card(fmt.Sprint(cs.missing), "no submission", sBad),
		card(fmt.Sprintf("%.1f%%", cs.mean), "mean", pctStyle(int(cs.mean))),
		card(fmt.Sprintf("%.1f%%", cs.median), "median", pctStyle(int(cs.median))),
		card(fmt.Sprint(cs.full), "at 100%", sGood),
		card(fmt.Sprint(nSubs), "submissions ("+m.sd+")", sAccent),
	})
	if gs := computeStats(m.groupStudents(), m.sd); gs.n > 0 {
		writeCards([]string{
			card(fmt.Sprint(gs.n), "your group", sAccent),
			card(fmt.Sprintf("%d/%d", gs.delivered, gs.n), "group submitted", sGood),
			card(fmt.Sprintf("%.1f%%", gs.mean), "group mean "+deltaCell(gs.mean-cs.mean, ""), pctStyle(int(gs.mean))),
			card(fmt.Sprintf("%.1f%%", gs.median), "group median "+deltaCell(gs.median-cs.median, ""), pctStyle(int(gs.median))),
		})
		b.WriteString(sDim.Render("  details in the Group tab (2)") + "\n")
	}
	b.WriteString("\n")

	// Grade histogram.
	buckets := make([]int, 11)
	for _, p := range percents {
		buckets[min(10, p/10)]++
	}
	maxB := 1
	for _, c := range buckets {
		maxB = max(maxB, c)
	}
	colW := max(30, (w-4)/2)
	barW := max(10, colW-18)
	hist := []string{sSectionHdr.Render("Grade distribution")}
	for i := 10; i >= 0; i-- {
		label := fmt.Sprintf("%3d-%-3d", i*10, i*10+9)
		if i == 10 {
			label = "   100 "
		}
		hist = append(hist, fmt.Sprintf("%s %s %s", sDim.Render(label),
			bar(float64(buckets[i])/float64(maxB), barW, pctStyle(i*10)), fmt.Sprint(buckets[i])))
	}

	// Submissions per day (last 14 days with activity).
	var keys []string
	for k := range days {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 11 {
		keys = keys[len(keys)-11:]
	}
	maxD := 1
	for _, k := range keys {
		maxD = max(maxD, days[k])
	}
	perDay := []string{sSectionHdr.Render("Submissions per day")}
	for _, k := range keys {
		t, _ := time.Parse("2006-01-02", k)
		perDay = append(perDay, fmt.Sprintf("%s %s %d", sDim.Render(t.Format("Mon 02/01")),
			bar(float64(days[k])/float64(maxD), barW, sAccent), days[k]))
	}

	left := lipgloss.NewStyle().Width(colW).Render(strings.Join(hist, "\n"))
	right := strings.Join(perDay, "\n")
	if w >= 2*colW+4 {
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, "    ", right) + "\n\n")
	} else {
		b.WriteString(left + "\n\n" + right + "\n\n")
	}

	// Suspicion summary.
	b.WriteString(sSectionHdr.Render("Possible cheating") + "\n")
	g := m.group()
	var risk [3]int
	for _, r := range m.riskScores() {
		if r.Score >= 0.5 {
			risk[r.Level()]++
		}
	}
	b.WriteString(fmt.Sprintf("  %s high · %s medium risk: high grade reached at the very start or end of the window, or in one jump\n",
		sBad.Bold(true).Render(fmt.Sprint(risk[analysis.High])), sWarn.Render(fmt.Sprint(risk[analysis.Medium]))))
	switch {
	case m.analyzing:
		b.WriteString("  " + sAccent.Render(fmt.Sprintf("analysing repos %d/%d…", m.anProg.done, m.anProg.total)) + "\n")
	case m.code == nil:
		b.WriteString("  " + sDim.Render("code not analysed: press A in the Repos tab to clone "+m.classLabel()) + "\n")
	default:
		var code, ai [3]int
		inGroup := 0
		for _, p := range m.codePairs() {
			code[p.Level()]++
			if pairInvolves(p.A, p.B, g) {
				inGroup++
			}
		}
		for _, s := range m.code.AI {
			if s.Score >= 0.5 {
				ai[s.Level()]++
			}
		}
		line := fmt.Sprintf("  %s high · %s medium · %s low pairs sharing code among %d repos",
			sBad.Bold(true).Render(fmt.Sprint(code[analysis.High])), sWarn.Render(fmt.Sprint(code[analysis.Medium])),
			sDim.Render(fmt.Sprint(code[analysis.Low])), len(m.code.Repos))
		if len(g) > 0 {
			line += fmt.Sprintf(" (%s involve your group)", sAccent.Render(fmt.Sprint(inGroup)))
		}
		b.WriteString(line + "\n")
		b.WriteString(fmt.Sprintf("  %s high · %s medium repos that look AI-generated\n",
			sBad.Bold(true).Render(fmt.Sprint(ai[analysis.High])), sWarn.Render(fmt.Sprint(ai[analysis.Medium]))))
	}
	b.WriteString(sDim.Render("  Ranking sorted by suspicion lists the students to check first; the Code tab lists the pairs") + "\n")
	if cs.errors > 0 {
		b.WriteString(fmt.Sprintf("\n  %s last submission errored: %d\n", sBad.Render("●"), cs.errors))
	}
	return b.String()
}
