package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type helpView struct {
	offset int
	lines  int // lines of the last render, to clamp scrolling
	height int
}

func (v *helpView) Title() string  { return "Help" }
func (v *helpView) Keys() []string { return []string{keyHint("↑/↓", "scroll")} }

func (v *helpView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "up", "k":
		v.offset--
	case "down", "j":
		v.offset++
	case "pgup":
		v.offset -= v.height
	case "pgdown", " ":
		v.offset += v.height
	case "g", "home":
		v.offset = 0
	case "G", "end":
		v.offset = v.lines
	}
	v.offset = max(0, min(v.offset, v.lines-v.height))
	return nil
}

func (v *helpView) View(m *Model, w, h int) string {
	sec := func(t string) string { return "\n" + sSectionHdr.Render(t) }
	k := func(key, d string) string { return "  " + sKey.Render(padRight(key, 14)) + d }
	text := strings.Join([]string{
		sec("General"),
		k("←/→ tab 1-6", "switch tab"),
		k("↑/↓ j/k", "move; pgup/pgdn, g/G top/bottom"),
		k("enter", "open details / comparison"),
		k("esc q", "back / quit"),
		k("m", "switch submission definition (submit ↔ archi)"),
		k("R", "reload from the API (ignores the cache)"),
		k("e", "export suspicious pairs to CSV"),
		k("?", "open / close this help"),
		sec("Reading the suspicion"),
		"  " + sDim.Render("Two students stand out when they fail the same tests that few others fail."),
		"  " + sDim.Render("Not attempted exercises, missing files and common mistakes are ignored."),
		"  " + sBold.Render("Overlap") + sDim.Render("    how much their failures are the same (rare tests count more)"),
		"  " + sBold.Render("Identical") + sDim.Render("  share of shared failures with exactly the same output"),
		"  " + sBold.Render("Evidence") + sDim.Render("   how many uncommon failures they share: one or two alone prove little"),
		"  " + sDim.Render("score = overlap × (½ + ½ identical) × evidence;  level ") + sBad.Bold(true).Render("high") +
			sDim.Render(" ≥ 80%, ") + sWarn.Render("medium") + sDim.Render(" ≥ 50%, low below"),
		"  " + sDim.Render("Each pair says why in words; the panel under Traces and the comparison show the three factors."),
		"  " + sDim.Render("These are hints: always read the code before concluding anything."),
		sec("Group  (--logins list + students marked with space)"),
		k("t", "section: Members (problems first) · Exercises (worst gap first)"),
		k("enter / H", "on a member: their detail / history"),
		sec("Tests / Traces"),
		k("s", "Tests: sort by pass rate, name or group gap (group vs class)"),
		k("f", "Traces: only pairs with a member of your group / everyone"),
		sec("Ranking"),
		k("s / r", "change sort / reverse"),
		k("/", "filter by login"),
		k("f", "show only the --logins list + marked students"),
		k("space", "mark student (marked students are cloned in Repos)"),
		k("c", "compare the 2 marked (or 1 marked + cursor)"),
		k("H", "history: grades in every TP / exam, by academic year + class (asks first)"),
		k("v", "show / hide columns (saved in ~/.config/tpi/config.json)"),
		sec("Student"),
		k("tab", "sections: Trace (graded submit) · Tags · Suspects"),
		k("enter", "on a submit tag: open its trace (downloaded on demand)"),
		k("[ / ]", "in a tag trace: older / newer submit tag"),
		k("enter / ← →", "fold / unfold a group"),
		k("n / N", "next / previous failed test"),
		k("F", "show failures only"),
		k("E / C", "expand / collapse everything"),
		k("H", "history of the student"),
		sec("History"),
		k("tab / ← →", "Summary · one tab per academic year + class (shows repeated years)"),
		k("s / r", "sort by activity, date or grade / reverse"),
		k("k", "every kind / only TPs / exams / retakes"),
		k("a", "also show activities without a submission"),
		k("U", "download the history again (asks first)"),
		sec("Repos"),
		k("a", "clone / update the marked students' repos at their graded submit tag"),
		k("u", "unmark the student under the cursor"),
		k("r", "remove the repo under the cursor (asks y to confirm)"),
	}, "\n")
	lines := strings.Split(text, "\n")
	v.lines, v.height = len(lines), max(1, h-1)
	if len(lines) <= h {
		v.offset = 0
		return text
	}
	v.offset = max(0, min(v.offset, len(lines)-v.height))
	end := min(len(lines), v.offset+v.height)
	more := sDim.Render("  ↓ more (↑/↓ to scroll)")
	if end == len(lines) {
		more = sDim.Render("  end")
	}
	return strings.Join(lines[v.offset:end], "\n") + "\n" + more
}

func padRight(s string, n int) string {
	return s + strings.Repeat(" ", max(1, n-lipgloss.Width(s)))
}
