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

type overviewView struct {
	onlyList bool // statistics of the --logins list and marked students only
}

func (v *overviewView) Title() string  { return "Overview" }
func (v *overviewView) Keys() []string { return []string{keyHint("f", "selection / whole promo")} }

func (v *overviewView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if k.String() == "f" {
		v.onlyList = !v.onlyList
	}
	return nil
}

// meanMedian returns the mean and the median of sorted percentages; the
// median of an even count is the mean of the two middle values.
func meanMedian(sorted []int) (mean, median float64) {
	n := len(sorted)
	if n == 0 {
		return 0, 0
	}
	for _, p := range sorted {
		mean += float64(p)
	}
	mean /= float64(n)
	if n%2 == 1 {
		median = float64(sorted[n/2])
	} else {
		median = float64(sorted[n/2-1]+sorted[n/2]) / 2
	}
	return mean, median
}

// fmtPct prints a percentage with one decimal, dropping a trailing ".0".
func fmtPct(p float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", p), ".0") + "%"
}

// promoPercents returns the sorted grades of every student who has one.
func (m *Model) promoPercents() []int {
	var ps []int
	for _, st := range m.ds.Students {
		if p := st.Percent(m.sd); p >= 0 {
			ps = append(ps, p)
		}
	}
	sort.Ints(ps)
	return ps
}

func card(value, label string, style lipgloss.Style) string {
	return sCard.Render(style.Bold(true).Render(value) + "\n" + sDim.Render(label))
}

func (v *overviewView) View(m *Model, w, h int) string {
	ds := m.ds
	var percents []int
	var nStudents, nSubs, delivered, full, errors int
	days := map[string]int{}
	var firstDay, lastDay time.Time
	for _, st := range ds.Students {
		if v.onlyList && !m.inSelection(st.Login) {
			continue
		}
		nStudents++
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
		last := st.Last(m.sd)
		if last == nil {
			continue
		}
		delivered++
		if last.Status == "ERROR" {
			errors++
		}
		if p := st.Percent(m.sd); p >= 0 {
			percents = append(percents, p)
			if p == 100 {
				full++
			}
		}
	}
	sort.Ints(percents)
	mean, median := meanMedian(percents)

	var b strings.Builder
	b.WriteString(sDim.Render(m.slug.Describe() + " · " + ds.Activity.URI))
	b.WriteString("\n")
	if win := ds.Window(m.sd); win != nil {
		b.WriteString(sDim.Render(fmt.Sprintf("%s open %s → close %s (%s)", m.sd, fmtTime(win.OpeningAt), fmtTime(win.ClosingAt),
			fmtDuration(win.ClosingAt.Sub(win.OpeningAt)))))
		b.WriteString("\n")
	} else {
		b.WriteString(sDim.Render("no submission window from the API for " + m.sd + ": timing uses the promo's first and last submissions"))
		b.WriteString("\n")
	}
	meanLabel, medianLabel := "mean", "median"
	if v.onlyList {
		b.WriteString(sAccent.Render(fmt.Sprintf("Selection only: %d students (--logins list + marked)", nStudents)))
		b.WriteString(sDim.Render(" · f for the whole promo"))
		b.WriteString("\n\n")
		if nStudents == 0 {
			b.WriteString(sDim.Render("No selection: use --logins list.txt or mark students with space in Ranking."))
			b.WriteString("\n")
			return b.String()
		}
		cMean, cMedian := meanMedian(m.promoPercents())
		meanLabel = "mean (promo " + fmtPct(cMean) + ")"
		medianLabel = "median (promo " + fmtPct(cMedian) + ")"
	} else {
		scope := "Whole promo"
		if len(m.list) > 0 || countTrue(m.marked) > 0 {
			scope += sDim.Render(" · f for the selection only")
		}
		b.WriteString(sAccent.Render(scope))
		b.WriteString("\n\n")
	}

	cards := []string{
		card(fmt.Sprint(nStudents), "students", sBold),
		card(fmt.Sprint(delivered), "submitted", sGood),
		card(fmt.Sprint(nStudents-delivered), "no submission", sBad),
		card(fmtPct(mean), meanLabel, pctStyle(int(mean))),
		card(fmtPct(median), medianLabel, pctStyle(int(median))),
		card(fmt.Sprint(full), "at 100%", sGood),
		card(fmt.Sprint(nSubs), "submissions ("+m.sd+")", sAccent),
	}
	// Wrap cards to the available width.
	var row []string
	for _, c := range cards {
		if lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, append(row, c)...)) > w && len(row) > 0 {
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, row...))
			b.WriteString("\n")
			row = nil
		}
		row = append(row, c)
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, row...))
	b.WriteString("\n\n")

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
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, "    ", right))
		b.WriteString("\n\n")
	} else {
		b.WriteString(left)
		b.WriteString("\n\n")
		b.WriteString(right)
		b.WriteString("\n\n")
	}

	// Suspicion summary.
	// Suspicion summary, limited to the selection in selection mode.
	in := func(l string) bool { return !v.onlyList || m.inSelection(l) }
	title := "Possible cheating"
	if v.onlyList {
		title += " (selection)"
	}
	b.WriteString(sSectionHdr.Render(title))
	b.WriteString("\n")
	var risk [3]int
	for l, r := range m.riskScores() {
		if in(l) && r.Score >= 0.5 {
			risk[r.Level()]++
		}
	}
	fmt.Fprintf(&b, "  %s high · %s medium risk: high grade reached at the very start or end of the window, or in one jump\n",
		sBad.Bold(true).Render(fmt.Sprint(risk[analysis.High])), sWarn.Render(fmt.Sprint(risk[analysis.Medium])))
	switch {
	case m.analyzing:
		b.WriteString("  ")
		b.WriteString(sAccent.Render(fmt.Sprintf("analysing repos %d/%d…", m.anProg.done, m.anProg.total)))
		b.WriteString("\n")
	case m.code == nil:
		b.WriteString("  ")
		b.WriteString(sDim.Render("code not analysed: press A in the Repos tab to clone " + m.classLabel()))
		b.WriteString("\n")
	default:
		var code, ai [3]int
		for _, p := range m.codePairs() {
			if in(p.A) || in(p.B) {
				code[p.Level()]++
			}
		}
		for l, s := range m.code.AI {
			if in(l) && s.Score >= 0.5 {
				ai[s.Level()]++
			}
		}
		fmt.Fprintf(&b, "  %s high · %s medium · %s low pairs sharing code among %d repos\n",
			sBad.Bold(true).Render(fmt.Sprint(code[analysis.High])), sWarn.Render(fmt.Sprint(code[analysis.Medium])),
			sDim.Render(fmt.Sprint(code[analysis.Low])), len(m.code.Repos))
		fmt.Fprintf(&b, "  %s high · %s medium repos that look AI-generated\n",
			sBad.Bold(true).Render(fmt.Sprint(ai[analysis.High])), sWarn.Render(fmt.Sprint(ai[analysis.Medium])))
	}
	b.WriteString(sDim.Render("  Ranking sorted by risk or suspicion lists the students to check first; the Code tab lists the pairs"))
	b.WriteString("\n")
	if errors > 0 {
		fmt.Fprintf(&b, "\n  %s last submission errored: %d\n", sBad.Render("●"), errors)
	}
	return b.String()
}
