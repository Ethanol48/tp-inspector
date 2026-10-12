package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/data"
)

// cleanItem is one deletable thing: an assignment's data or its repos.
type cleanItem struct {
	slug   string
	repos  bool // false: API responses and traces
	bytes  int64
	detail string
	marked bool
}

// CleanModel is the `tpi clean` interface.
type CleanModel struct {
	root    string
	items   []cleanItem
	cursor  int
	confirm bool
	status  string
	err     error
	w, h    int
}

// NewClean creates the cleanup interface for a cache root.
func NewClean(root string) *CleanModel {
	c := &CleanModel{root: root}
	c.scan()
	return c
}

// RunClean starts the cleanup interface.
func RunClean(root string) error {
	_, err := tea.NewProgram(NewClean(root), tea.WithAltScreen()).Run()
	return err
}

func (c *CleanModel) scan() {
	entries, err := data.ScanCache(c.root)
	c.err = err
	c.items = nil
	for _, e := range entries {
		if e.DataBytes > 0 {
			detail := fmt.Sprintf("%d traces + API responses", e.Traces)
			if e.Slug == data.HistoriesDir {
				detail = "student histories (H)"
			}
			c.items = append(c.items, cleanItem{slug: e.Slug, bytes: e.DataBytes, detail: detail})
		}
		if len(e.Repos) > 0 {
			names := strings.Join(e.Repos[:min(4, len(e.Repos))], ", ")
			if len(e.Repos) > 4 {
				names += fmt.Sprintf(" … (+%d)", len(e.Repos)-4)
			}
			c.items = append(c.items, cleanItem{slug: e.Slug, repos: true, bytes: e.ReposBytes,
				detail: fmt.Sprintf("%d repos: %s", len(e.Repos), names)})
		}
	}
	c.cursor = max(0, min(c.cursor, len(c.items)-1))
}

func (c *CleanModel) selected() (n int, bytes int64) {
	for _, it := range c.items {
		if it.marked {
			n++
			bytes += it.bytes
		}
	}
	return
}

func (c *CleanModel) Init() tea.Cmd { return nil }

func (c *CleanModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.w, c.h = msg.Width, msg.Height
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return c, tea.Quit
		}
		if c.confirm {
			c.confirm = false
			if key == "y" {
				c.delete()
			} else {
				c.status = "Cancelled"
			}
			return c, nil
		}
		c.status = ""
		switch key {
		case "q", "esc":
			return c, tea.Quit
		case "up":
			c.cursor = max(0, c.cursor-1)
		case "down":
			c.cursor = min(len(c.items)-1, c.cursor+1)
		case " ", "x":
			if c.cursor < len(c.items) {
				c.items[c.cursor].marked = !c.items[c.cursor].marked
				c.cursor = min(len(c.items)-1, c.cursor+1)
			}
		case "a":
			for i := range c.items {
				c.items[i].marked = true
			}
		case "n":
			for i := range c.items {
				c.items[i].marked = false
			}
		case "r":
			for i := range c.items {
				c.items[i].marked = c.items[i].repos
			}
		case "d", "enter":
			if n, _ := c.selected(); n > 0 {
				c.confirm = true
			} else {
				c.status = "Nothing selected (space to select)"
			}
		}
	}
	return c, nil
}

func (c *CleanModel) delete() {
	var done int
	var freed int64
	for _, it := range c.items {
		if !it.marked {
			continue
		}
		var err error
		if it.repos {
			err = data.RemoveRepos(c.root, it.slug)
		} else {
			err = data.RemoveData(c.root, it.slug)
		}
		if err != nil {
			c.status = "Error: " + err.Error()
			c.scan()
			return
		}
		done++
		freed += it.bytes
	}
	// The global activities list goes with the last assignment.
	if len(c.items) == done {
		_ = os.Remove(filepath.Join(c.root, "activities.json"))
	}
	c.scan()
	c.status = fmt.Sprintf("Removed %d item(s), freed %s", done, data.HumanBytes(freed))
}

func (c *CleanModel) View() string {
	if c.w == 0 {
		return ""
	}
	var total int64
	for _, it := range c.items {
		total += it.bytes
	}
	var b strings.Builder
	b.WriteString(" " + sTitle.Render("tpi clean") + sDim.Render(fmt.Sprintf(" · %s · %s cached", c.root, data.HumanBytes(total))) + "\n\n")
	if c.err != nil {
		b.WriteString(" " + sBad.Render(c.err.Error()) + "\n")
	}
	if len(c.items) == 0 {
		b.WriteString(" " + sDim.Render("The cache is empty.") + "\n")
	}
	lastSlug := ""
	for i, it := range c.items {
		if it.slug != lastSlug {
			if lastSlug != "" {
				b.WriteString("\n")
			}
			b.WriteString(" " + sBold.Render(it.slug) + "\n")
			lastSlug = it.slug
		}
		prefix := "  "
		if i == c.cursor {
			prefix = sAccent.Bold(true).Render("❯ ")
		}
		box := sDim.Render("[ ]")
		if it.marked {
			box = sBad.Render("[x]")
		}
		kind := "TP data"
		if it.repos {
			kind = "repos"
		}
		line := fmt.Sprintf(" %s%s %-8s %8s  %s", prefix, box, kind, data.HumanBytes(it.bytes), sDim.Render(it.detail))
		b.WriteString(lipgloss.NewStyle().MaxWidth(c.w).Render(line) + "\n")
	}
	b.WriteString("\n")
	switch {
	case c.confirm:
		n, bytes := c.selected()
		b.WriteString(" " + sBad.Bold(true).Render(fmt.Sprintf("Delete %d item(s), %s? ", n, data.HumanBytes(bytes))) +
			keyHint("y", "yes") + sDim.Render(" · any other key cancels"))
	case c.status != "":
		b.WriteString(" " + sAccent.Render(c.status))
	default:
		n, bytes := c.selected()
		hints := []string{keyHint("space", "select"), keyHint("a", "all"), keyHint("r", "all repos"),
			keyHint("n", "none"), keyHint("d", fmt.Sprintf("delete (%d, %s)", n, data.HumanBytes(bytes))), keyHint("q", "quit")}
		b.WriteString(" " + strings.Join(hints, sDim.Render(" · ")))
	}
	b.WriteString("\n\n " + sDim.Render("TP data = cached API responses and traces (downloaded again on next run). Your login and settings are kept."))
	return b.String()
}
