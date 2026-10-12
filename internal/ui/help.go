package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type helpView struct{}

func (v *helpView) Title() string                         { return "Help" }
func (v *helpView) Keys() []string                        { return nil }
func (v *helpView) Update(m *Model, k tea.KeyMsg) tea.Cmd { return nil }

func (v *helpView) View(m *Model, w, h int) string {
	sec := func(t string) string { return "\n" + sSectionHdr.Render(t) }
	k := func(key, d string) string { return "  " + sKey.Render(padRight(key, 14)) + d }
	return strings.Join([]string{
		sec("General"),
		k("←/→ tab 1-5", "switch tab"),
		k("↑/↓ j/k", "move; pgup/pgdn, g/G top/bottom"),
		k("enter", "open details / comparison"),
		k("esc q", "back / quit"),
		k("m", "switch submission definition (submit / submission ↔ archi)"),
		k("R", "reload from the API (ignores the cache)"),
		k("e", "export suspicious pairs to CSV"),
		k("?", "open / close this help"),
		sec("Overview"),
		k("f", "statistics of the --logins list + marked students / whole promo"),
		sec("Ranking"),
		k("s / r", "change sort / reverse"),
		k("/", "filter by login"),
		k("f", "show only the --logins list + marked students"),
		k("space", "mark student (marked students are cloned in Repos)"),
		k("c", "compare the 2 marked (or 1 marked + cursor)"),
		k("H", "history: grades in every TP / exam, by academic year + promo (asks first)"),
		k("v", "show / hide columns (saved in ~/.config/tpi/config.json)"),
		sec("Student"),
		k("tab", "sections: Trace (graded submission) · Tags · Suspects"),
		k("enter", "on a tag of the graded definition (submit, submission…): open its trace"),
		k("[ / ]", "in a tag trace: older / newer graded tag"),
		k("enter / ← →", "fold / unfold a group"),
		k("n / N", "next / previous failed test"),
		k("F", "show failures only"),
		k("E / C", "expand / collapse everything"),
		k("H", "history of the student"),
		sec("History"),
		k("tab / ← →", "Summary · one tab per academic year + promo (shows repeated years)"),
		k("s / r", "sort by activity, date or grade / reverse"),
		k("k", "every kind / only TPs / exams / retakes"),
		k("a", "also show activities without a submission"),
		k("U", "download the history again (asks first)"),
		sec("Repos"),
		k("a", "clone / update the marked students' repos at their graded tag"),
		k("u", "unmark the student under the cursor"),
		k("r", "remove the repo under the cursor (asks y to confirm)"),
		sec("Reading the scores"),
		"  " + sDim.Render("Traces: IDF-weighted Jaccard of informative failed tests (rare failures weigh more)"),
		"  " + sDim.Render("        × bonus for identical output × amount of shared evidence."),
		"  " + sDim.Render("These are hints: always read the code before concluding anything."),
	}, "\n")
}

func padRight(s string, n int) string {
	return s + strings.Repeat(" ", max(1, n-lipgloss.Width(s)))
}
