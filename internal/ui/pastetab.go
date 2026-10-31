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

// Levels, as in analysis.LevelOf: high from 80 %, medium from 50 %.
const (
	pasteMedium = 0.5
	pasteHigh   = 0.8
)

var pasteSorts = []string{"score", "pace", "big commit", "duplicates"}

// pasteOf returns the paste and pace report of a student's repo.
func (m *Model) pasteOf(login string) (analysis.PasteReport, bool) {
	r, ok := m.paste[login]
	return r, ok
}

// pasteKey is the value the tab sorts and filters on.
func pasteKey(r analysis.PasteReport, mode int) float64 {
	switch mode {
	case 1:
		return r.Pace
	case 2:
		return r.Paste
	case 3:
		return r.Dup
	}
	return r.Score
}

func pasteCell(x float64) string {
	if x >= pasteMedium {
		return sBad.Render(scorePct(x))
	}
	return sDim.Render(scorePct(x))
}

// pasteView lists the repos with copy/paste or code written too fast.
type pasteView struct {
	t         Table
	gen       int
	onlyGroup bool // show only the group (when there is one)
	filtered  bool // onlyGroup applied at the last rebuild
	onlyFlag  bool // hide what is below the medium level
	sort      int  // index in pasteSorts
}

func (v *pasteView) Title() string { return "Paste" }

func (v *pasteView) Keys() []string {
	return []string{keyHint("enter", "details"), keyHint("s", "sort"), keyHint("f", "group / everyone"), keyHint("h", "flagged / all")}
}

func (v *pasteView) rebuild(m *Model) {
	v.t.Cols = []Column{
		{Title: "#", Width: 4, Right: true},
		{Title: "Level", Width: 6},
		{Title: "Student", Width: 24},
		{Title: "Pace", Width: 5, Right: true},
		{Title: "Paste", Width: 5, Right: true},
		{Title: "Dup", Width: 5, Right: true},
		{Title: "Why", Width: 0},
		{Title: "Lines", Width: 6, Right: true},
		{Title: "Span", Width: 8, Right: true},
		{Title: "Grade", Width: 5, Right: true},
	}
	g := m.group()
	v.filtered = v.onlyGroup && len(g) > 0
	name := func(l string) string {
		if g[l] {
			return sAccent.Render("◦ ") + l
		}
		return "  " + l
	}

	var logins []string
	for l := range m.paste {
		logins = append(logins, l)
	}
	sort.Slice(logins, func(i, j int) bool {
		a, b := pasteKey(m.paste[logins[i]], v.sort), pasteKey(m.paste[logins[j]], v.sort)
		if a != b {
			return a > b
		}
		return logins[i] < logins[j]
	})

	var rows []Row
	for _, l := range logins {
		r := m.paste[l]
		if v.filtered && !g[l] {
			continue
		}
		if v.onlyFlag && pasteKey(r, v.sort) < pasteMedium {
			continue
		}
		grade := -1
		if st := m.student(l); st != nil {
			grade = st.Percent(m.sd)
		}
		why := r.Reason()
		if r.NoHistory {
			why += " (no git history)"
		}
		lines, span := sDim.Render("·"), sDim.Render("·")
		if !r.NoHistory {
			lines = fmt.Sprint(r.Lines)
		}
		if r.Span > 0 {
			span = fmtDuration(r.Span)
		}
		rows = append(rows, Row{Key: l, Cells: []string{
			sDim.Render(fmt.Sprint(len(rows) + 1)), levelCell(r.Score), name(l),
			pasteCell(r.Pace), pasteCell(r.Paste), pasteCell(r.Dup),
			why, lines, span, pctCell(grade),
		}})
	}

	switch {
	case m.analyzing:
		v.t.Empty = fmt.Sprintf("Analysing the repos… %d/%d", m.anProg.done, m.anProg.total)
	case m.paste == nil:
		v.t.Empty = "No repo analysed: press A in the Repos tab to clone " + m.classLabel() + " (or a for the marked students)."
	case v.filtered:
		v.t.Empty = "No analysed repo belongs to your group (f to show everyone)."
	case v.onlyFlag:
		v.t.Empty = "Nothing reaches the medium level (h to show every repo)."
	default:
		v.t.Empty = "No repo analysed."
	}
	v.t.SetRows(rows)
}

func (v *pasteView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.t.HandleKey(k.String()) {
		return nil
	}
	switch k.String() {
	case "f":
		v.onlyGroup = !v.onlyGroup
		v.gen = -1
	case "h":
		v.onlyFlag = !v.onlyFlag
		v.gen = -1
	case "s":
		v.sort = (v.sort + 1) % len(pasteSorts)
		v.gen = -1
	case "enter":
		if sel := v.t.Selected(); sel != nil {
			m.push(&pasteDetailView{login: sel.Key})
		}
	}
	return nil
}

func (v *pasteView) View(m *Model, w, h int) string {
	if v.gen != m.gen || m.analyzing {
		v.rebuild(m)
		v.gen = m.gen
	}
	head := sDim.Render("Pace: commits adding many lines minutes after the previous one, or a whole TP typed at an impossible speed. Paste: one commit holding a big block. Dup: the same code repeated in the files. Hints, not proof.")
	if len(m.paste) > 0 {
		high, med := 0, 0
		for _, r := range m.paste {
			switch {
			case r.Score >= pasteHigh:
				high++
			case r.Score >= pasteMedium:
				med++
			}
		}
		head += "\n" + sDim.Render(fmt.Sprintf("%d repos analysed (only the repos on disk) · %d high · %d medium · sorted by %s", len(m.paste), high, med, pasteSorts[v.sort]))
	}
	switch {
	case v.filtered:
		head += "\n" + sAccent.Render(fmt.Sprintf("Only your group (◦): %d", len(v.t.Rows))) + sDim.Render(" · f to show everyone")
	case m.hasGroup():
		head += "\n" + sDim.Render("Everyone · f to show only your group (◦)")
	}
	if v.onlyFlag {
		head += "\n" + sAccent.Render("Medium and high only") + sDim.Render(" · h to show every repo")
	}
	head += "\n\n"

	why := ""
	if sel := v.t.Selected(); sel != nil {
		if r, ok := m.pasteOf(sel.Key); ok && len(r.Reasons) > 0 {
			var ls []string
			for i, s := range r.Reasons {
				if i == 4 {
					break
				}
				ls = append(ls, "• "+s)
			}
			why = "\n" + sSectionHdr.Render("Why "+sel.Key) + "\n" +
				lipgloss.NewStyle().MaxWidth(w).Render(strings.Join(ls, "\n"))
		}
	}
	used := strings.Count(head, "\n") + strings.Count(why, "\n")
	return head + v.t.View(w, max(3, h-used)) + why
}

// pasteDetailView shows the fast commits and the repeated blocks of a repo.
type pasteDetailView struct {
	login  string
	offset int
}

func (v *pasteDetailView) Title() string { return "Paste · " + v.login }

func (v *pasteDetailView) Keys() []string { return []string{keyHint("↑/↓", "scroll")} }

func (v *pasteDetailView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "up":
		v.offset--
	case "down":
		v.offset++
	case "pgup":
		v.offset -= 10
	case "pgdown":
		v.offset += 10
	case "g", "home":
		v.offset = 0
	case "G", "end":
		v.offset = 1 << 20 // clamped when drawn
	}
	v.offset = max(0, v.offset)
	return nil
}

func (v *pasteDetailView) View(m *Model, w, h int) string {
	r, ok := m.pasteOf(v.login)
	if !ok {
		return sDim.Render("No analysis for " + v.login + ": clone and analyse the repo first.")
	}

	lines := []string{
		sBold.Render(v.login) + "  " + levelCell(r.Score) + " " + sDim.Render(scorePct(r.Score)),
		sDim.Render(fmt.Sprintf("pace %s · paste %s · duplicates %s", scorePct(r.Pace), scorePct(r.Paste), scorePct(r.Dup))),
	}

	lines = append(lines, "", sSectionHdr.Render("Pace"))
	if r.NoHistory {
		lines = append(lines, sDim.Render("  git history unreadable"))
	} else {
		lines = append(lines, fmt.Sprintf("  %d commits, %d lines of C / Python added", r.Commits, r.Lines))
		if r.SkippedRoot {
			lines = append(lines, sDim.Render("  first commit left out: the same size in many repos, the subject's skeleton"))
		}
		if r.Span > 0 {
			lines = append(lines, fmt.Sprintf("  first to last commit: %s", fmtDuration(r.Span)))
		}
		if r.Rate > 0 {
			lines = append(lines, fmt.Sprintf("  %.1f lines per minute over that time (a person types a few)", r.Rate))
		}
		if r.MaxRate > 0 {
			lines = append(lines, fmt.Sprintf("  fastest commit: %.0f lines per minute", r.MaxRate))
		}
		lines = append(lines, fmt.Sprintf("  biggest commit: %d lines · %d commits of %d lines or more", r.Biggest, r.BigCommits, analysis.BigCommit))
		if len(r.Bursts) == 0 {
			lines = append(lines, sDim.Render("  no burst commit"))
		}
		for i, b := range r.Bursts {
			if i == 15 {
				lines = append(lines, sDim.Render(fmt.Sprintf("  … and %d more", len(r.Bursts)-i)))
				break
			}
			lines = append(lines, fmt.Sprintf("  %s  %s  +%d lines, %s after the previous commit",
				sDim.Render(b.At.Local().Format("2 Jan 15:04")), sAccent.Render(b.Hash), b.Lines, b.Gap.Round(time.Second)))
		}
	}

	lines = append(lines, "", sSectionHdr.Render("Copy / paste inside the files"))
	lines = append(lines, fmt.Sprintf("  %d of %d lines of code repeated elsewhere in the repo (%.0f%%)", r.DupLines, r.CodeLines, r.DupRatio*100))
	lines = append(lines, sDim.Render("  names, numbers and strings ignored; blocks of the subject's skeleton left out"))
	for _, b := range r.Dups {
		lines = append(lines, fmt.Sprintf("  %s  %s", sBold.Render(fmt.Sprintf("%s:%d-%d", b.File, b.Start, b.End)), sDim.Render(fmt.Sprintf("%d lines, also at %s", b.Lines, b.Other))))
	}

	v.offset = min(v.offset, max(0, len(lines)-h))
	return strings.Join(lines[v.offset:min(len(lines), v.offset+h)], "\n")
}
