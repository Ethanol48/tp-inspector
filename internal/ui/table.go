package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Column of a Table. Width 0 makes the column take the remaining space.
type Column struct {
	Title string
	Width int
	Right bool
}

// Row cells may contain ANSI styling.
type Row struct {
	Key   string
	Cells []string
}

// Table is a scrollable table with a cursor, supporting styled cells.
type Table struct {
	Cols    []Column
	Rows    []Row
	Empty   string // shown when there are no rows
	cursor  int
	offset  int
	visible int
}

// SetRows replaces rows, keeping the cursor on the same key when possible.
func (t *Table) SetRows(rows []Row) {
	var key string
	if sel := t.Selected(); sel != nil {
		key = sel.Key
	}
	t.Rows = rows
	t.cursor = min(t.cursor, max(0, len(rows)-1))
	if key != "" {
		for i, r := range rows {
			if r.Key == key {
				t.cursor = i
				break
			}
		}
	}
}

func (t *Table) Selected() *Row {
	if t.cursor < 0 || t.cursor >= len(t.Rows) {
		return nil
	}
	return &t.Rows[t.cursor]
}

func (t *Table) Cursor() int { return t.cursor }

// HandleKey moves the cursor; it reports whether the key was used.
func (t *Table) HandleKey(k string) bool {
	page := max(1, t.visible-1)
	switch k {
	case "up", "k":
		t.cursor--
	case "down", "j":
		t.cursor++
	case "pgup", "ctrl+u":
		t.cursor -= page
	case "pgdown", "ctrl+d", " ":
		if k == " " {
			return false
		}
		t.cursor += page
	case "home", "g":
		t.cursor = 0
	case "end", "G":
		t.cursor = len(t.Rows) - 1
	default:
		return false
	}
	t.cursor = max(0, min(t.cursor, len(t.Rows)-1))
	return true
}

func fit(s string, w int, right bool) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	pad := strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
	if right {
		return pad + s
	}
	return s + pad
}

// View renders the table in w×h cells.
func (t *Table) View(w, h int) string {
	const gap = 2
	widths := make([]int, len(t.Cols))
	fixed, flex := 2, -1 // 2 = cursor gutter
	for i, c := range t.Cols {
		widths[i] = c.Width
		if c.Width == 0 && flex < 0 {
			flex = i
		}
		fixed += c.Width + gap
	}
	if flex >= 0 {
		widths[flex] = max(8, w-fixed)
	}

	line := func(cells []string, prefix string) string {
		var b strings.Builder
		b.WriteString(prefix)
		for i, c := range t.Cols {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", gap))
			}
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			b.WriteString(fit(cell, widths[i], c.Right))
		}
		return ansi.Truncate(b.String(), w, "")
	}

	titles := make([]string, len(t.Cols))
	for i, c := range t.Cols {
		titles[i] = sHeader.Render(c.Title)
	}
	out := []string{line(titles, "  ")}

	t.visible = max(1, h-2) // header + position line
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+t.visible {
		t.offset = t.cursor - t.visible + 1
	}
	t.offset = max(0, min(t.offset, max(0, len(t.Rows)-t.visible)))

	if len(t.Rows) == 0 {
		msg := t.Empty
		if msg == "" {
			msg = "Nothing to show."
		}
		out = append(out, "", "  "+sDim.Render(msg))
		return strings.Join(out, "\n")
	}
	for i := t.offset; i < min(len(t.Rows), t.offset+t.visible); i++ {
		prefix := "  "
		if i == t.cursor {
			prefix = sAccent.Bold(true).Render("❯ ")
		}
		out = append(out, line(t.Rows[i].Cells, prefix))
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	out = append(out, sDim.Render(fmt.Sprintf("  %d/%d", t.cursor+1, len(t.Rows))))
	return strings.Join(out, "\n")
}
