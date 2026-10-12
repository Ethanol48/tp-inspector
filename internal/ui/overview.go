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
	var percents []int
	var nSubs, delivered, full, errors int
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
	mean, median := 0.0, 0
	for _, p := range percents {
		mean += float64(p)
	}
	if len(percents) > 0 {
		mean /= float64(len(percents))
		median = percents[len(percents)/2]
	}

	var b strings.Builder
	b.WriteString(sDim.Render(m.slug.Describe()+" · "+ds.Activity.URI) + "\n\n")

	cards := []string{
		card(fmt.Sprint(len(ds.Students)), "students", sBold),
		card(fmt.Sprint(delivered), "submitted", sGood),
		card(fmt.Sprint(len(ds.Students)-delivered), "no submission", sBad),
		card(fmt.Sprintf("%.1f%%", mean), "mean", pctStyle(int(mean))),
		card(fmt.Sprintf("%d%%", median), "median", pctStyle(median)),
		card(fmt.Sprint(full), "at 100%", sGood),
		card(fmt.Sprint(nSubs), "submissions ("+m.sd+")", sAccent),
	}
	// Wrap cards to the available width.
	var row []string
	for _, c := range cards {
		if lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, append(row, c)...)) > w && len(row) > 0 {
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, row...) + "\n")
			row = nil
		}
		row = append(row, c)
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, row...) + "\n\n")

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
	b.WriteString(sSectionHdr.Render("Possible plagiarism") + "\n")
	var byLevel [3]int
	for _, p := range m.pairsTraces() {
		byLevel[p.Level()]++
	}
	b.WriteString(fmt.Sprintf("  %s high · %s medium · %s low pairs with similar traces\n",
		sBad.Bold(true).Render(fmt.Sprint(byLevel[analysis.High])), sWarn.Render(fmt.Sprint(byLevel[analysis.Medium])),
		sDim.Render(fmt.Sprint(byLevel[analysis.Low]))))
	b.WriteString(sDim.Render("  see the Traces tab: each pair says why it stands out") + "\n")
	if errors > 0 {
		b.WriteString(fmt.Sprintf("\n  %s last submission errored: %d\n", sBad.Render("●"), errors))
	}
	return b.String()
}
