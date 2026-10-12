package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/data"
)

// reposView clones the repos of the students marked in Ranking, at the tag
// of their graded submit submission, and lists where they are.
type reposView struct {
	t       Table
	gen     int
	confirm string // login waiting for y to be removed
}

func (v *reposView) Title() string { return "Repos" }
func (v *reposView) Keys() []string {
	if v.confirm != "" {
		return []string{keyHint("y", "remove "+v.confirm), keyHint("any key", "cancel")}
	}
	return []string{keyHint("a", "clone/update marked"), keyHint("r", "remove repo"), keyHint("enter", "student"), keyHint("u", "unmark")}
}

func (v *reposView) rebuild(m *Model) {
	v.t.Cols = []Column{
		{Title: "Login", Width: 34},
		{Title: "Tag", Width: 24},
		{Title: "Status", Width: 10},
		{Title: "Commit", Width: 40},
		{Title: "Path", Width: 0},
	}
	var rows []Row
	for _, l := range m.repoLogins() {
		st := m.student(l)
		tag := sDim.Render("no submission")
		if st != nil {
			if last := st.Last(m.ds.Graded); last != nil {
				tag = last.Ref
			}
		}
		label := l
		if !m.marked[l] {
			label += sDim.Render(" (not marked)")
		}
		status, commit, path := sDim.Render("not cloned"), "", ""
		if r, ok := m.clones[l]; ok {
			if r.Err != nil {
				status, commit = sBad.Render("error"), sBad.Render(r.Err.Error())
			} else {
				status, commit, path = sGood.Render("cloned"), r.Commit, sDim.Render(r.Dir)
				if r.Ref != tag {
					status = sWarn.Render("outdated") // a newer submission was graded since
				}
			}
		}
		rows = append(rows, Row{Key: l, Cells: []string{label, tag, status, commit, path}})
	}
	v.t.Empty = "No student marked and no repo on disk: mark students with space in Ranking, then press a here."
	v.t.SetRows(rows)
}

func (v *reposView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.confirm != "" {
		login := v.confirm
		v.confirm = ""
		if k.String() != "y" {
			return m.setStatus("Cancelled")
		}
		if err := data.RemoveRepo(m.loader.Cache.Root, m.ds.Activity.Slug, login); err != nil {
			return m.setStatus("Could not remove: " + err.Error())
		}
		delete(m.clones, login)
		m.gen++
		return m.setStatus("Removed the repo of " + login)
	}
	switch k.String() {
	case "a":
		return m.startClone()
	case "r":
		if sel := v.t.Selected(); sel != nil {
			if _, ok := m.clones[sel.Key]; !ok {
				return m.setStatus(sel.Key + " has no cloned repo")
			}
			v.confirm = sel.Key
		}
		return nil
	case "u":
		if sel := v.t.Selected(); sel != nil {
			delete(m.marked, sel.Key)
			m.gen++
		}
		return nil
	case "enter":
		if sel := v.t.Selected(); sel != nil {
			m.push(newDetailView(sel.Key))
		}
		return nil
	}
	v.t.HandleKey(k.String())
	return nil
}

func (v *reposView) View(m *Model, w, h int) string {
	if m.cloning {
		p := m.cloneProg
		line := sBold.Render("Cloning repos") + sDim.Render(fmt.Sprintf("  %d/%d", p.done, p.total))
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
			line+"\n"+bar(float64(p.done)/float64(max(1, p.total)), min(50, w-4), sAccent))
	}
	if v.gen != m.gen {
		v.rebuild(m)
		v.gen = m.gen
	}
	if v.confirm != "" {
		h -= 2
	}
	head := []string{
		sDim.Render(fmt.Sprintf("%d marked · cloned at the tag of their graded submission into %s", len(m.cloneSelection()), m.repoRoot())),
		sDim.Render("Only marked students are cloned (space in Ranking); repos already on disk are listed too so you can remove them."),
	}
	out := strings.Join(head, "\n") + "\n\n" + v.t.View(w, h-3)
	if v.confirm != "" {
		out += "\n\n" + sBad.Bold(true).Render("Remove "+data.RepoDir(m.loader.Cache.Root, m.ds.Activity.Slug, v.confirm)+"? ") + keyHint("y", "yes") + sDim.Render(" · any other key cancels")
	}
	return out
}
