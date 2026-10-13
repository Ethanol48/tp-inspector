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
		k("m", "switch submission definition (submit / submission ↔ archi)"),
		k("R", "reload from the API (ignores the cache)"),
		k("e", "export risk, code pairs and AI scores to CSV"),
		k("?", "open / close this help"),
		sec("Reading the suspicion"),
		"  " + sDim.Render("Suspicion = 1 − (1−risk)(1−code)(1−AI): signals add up. Each says why in words. Hints only: read the code."),
		"  " + sBold.Render("Risk") + sDim.Render("   from the submissions of the whole class, no clone needed:"),
		"  " + sDim.Render("       (grade/100)² × (0.4 + 0.6 × min(1, timing + jump)). Timing: the final grade reached in the first"),
		"  " + sDim.Render("       10% or the last 3% of the submission window. Jump (≤ 0.5): first submission already ≥ 90%, or +80 points at once."),
		"  " + sBold.Render("Code") + sDim.Render("   function bodies alike between two repos once names and layout are ignored (winnowing, like MOSS);"),
		"  " + sDim.Render("       code most repos share (the skeleton) is ignored. Functions identical but for whitespace / comments count extra."),
		"  " + sBold.Render("AI") + sDim.Render("     style far from the class (comments, doc tags, guard clauses, long names, rare constructs,"),
		"  " + sDim.Render("       history in one commit) plus absolute tells (— → emojis, @param docs, generated commit messages)."),
		"  " + sDim.Render("       A commit crediting an AI tool (Co-Authored-By: Claude, Generated with…, copilot[bot]) sets it ≥ 95%."),
		"  " + sDim.Render("Code and AI only cover repos on disk: A in Repos clones the class.  Level ") + sBad.Bold(true).Render("high") +
			sDim.Render(" ≥ 80%, ") + sWarn.Render("medium") + sDim.Render(" ≥ 50%, low below."),
		sec("Overview"),
		k("f", "statistics of the --logins list + marked students / whole class"),
		sec("Group  (--logins list + students marked with space)"),
		k("t", "section: Members (problems first) · Exercises (worst gap first)"),
		k("enter / H", "on a member: their detail / history"),
		sec("Tests / Code"),
		k("s", "Tests: sort by pass rate, name or group gap (group vs class)"),
		k("f", "Code: only pairs with a member of your group / everyone"),
		k("enter", "Code: compare the pair, matched functions side by side"),
		sec("Ranking"),
		k("s / r", "change sort (risk: everyone, no clone needed; suspicion: analysed first) / reverse"),
		k("/", "filter by login"),
		k("f", "show only the --logins list + marked students"),
		k("space", "mark student (a in Repos clones the marked students)"),
		k("c", "compare the 2 marked (or 1 marked + cursor)"),
		k("H", "history: grades in every TP / exam, by academic year + class (asks first)"),
		k("v", "show / hide columns (saved in ~/.config/tpi/config.json)"),
		sec("Student"),
		k("tab", "sections: Trace (graded submit / submission) · Tags · Suspects (risk, AI, code pairs)"),
		k("space", "in Tags / Suspects: mark / unmark the student"),
		k("enter", "in Suspects, on a code pair: compare the two students"),
		k("enter", "on a submit-* / submission-* tag: open its trace (downloaded on demand)"),
		k("[ / ]", "in a tag trace: older / newer graded tag"),
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
		k("A", "clone / update the class: --logins + marked, else the whole promotion (asks y)"),
		k("K", "unlock your SSH key (ssh-add) when cloning was refused"),
		k("a", "clone / update the marked students' repos at their graded submit-* / submission-* tag"),
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
