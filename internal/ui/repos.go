package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/data"
)

// reposView clones the repos of the students marked in Ranking (or of the
// class: --logins, else the whole promotion), at the tag of their graded
// submission (submit-* for TPs, submission-* for exams), and lists where they are. Every repo on disk is then
// analysed for shared and generated code.
type reposView struct {
	t          Table
	gen        int
	confirm    string // login waiting for y to be removed
	confirmAll int    // repos waiting for y to clone the class
	locked     bool   // SSH key locked at the last render, for Keys
	listed     bool   // A clones the --logins list rather than the promotion
}

func (v *reposView) Title() string { return "Repos" }
func (v *reposView) Keys() []string {
	if v.confirm != "" {
		return []string{keyHint("y", "remove "+v.confirm), keyHint("any key", "cancel")}
	}
	if v.confirmAll > 0 {
		return []string{keyHint("y", fmt.Sprintf("clone %d repos", v.confirmAll)), keyHint("any key", "cancel")}
	}
	what := "whole promotion"
	if v.listed {
		what = "--logins class"
	}
	k := []string{keyHint("A", "clone/update "+what), keyHint("a", "clone/update marked")}
	if v.locked {
		k = append([]string{keyHint("K", "unlock SSH key")}, k...)
	}
	return append(k, keyHint("r", "remove repo"), keyHint("enter", "student"), keyHint("u", "unmark"))
}

func (v *reposView) rebuild(m *Model) {
	v.t.Cols = []Column{
		{Title: "Login", Width: 34},
		{Title: "Tag", Width: 24},
		{Title: "Status", Width: 10},
		{Title: "Commit", Width: 40},
		{Title: "Code", Width: 6},
		{Title: "AI", Width: 6},
		{Title: "Path", Width: 0},
	}
	best := m.codeBest
	var rows []Row
	for _, l := range m.repoLogins() {
		st := m.student(l)
		tag := sDim.Render("no " + m.gradedSD())
		if st != nil {
			if last := st.Last(m.gradedSD()); last != nil {
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
					status = sWarn.Render("outdated") // a newer submit was graded since
				}
			}
		}
		code, ai := sDim.Render("·"), sDim.Render("·")
		if m.code != nil && m.code.Repos[l] != nil {
			code = levelCell(best[l])
			if s, ok := m.aiOf(l); ok {
				ai = levelCell(s.Score)
			}
		}
		rows = append(rows, Row{Key: l, Cells: []string{label, tag, status, commit, code, ai, path}})
	}
	v.t.Empty = "No repo on disk: press A to clone " + m.classLabel() + " (needed to find shared code), or mark students with space in Ranking and press a."
	v.t.SetRows(rows)
}

func (v *reposView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.confirmAll > 0 {
		v.confirmAll = 0
		if k.String() != "y" {
			return m.setStatus("Cancelled")
		}
		return m.startClone(m.classToClone())
	}
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
		return tea.Batch(m.startAnalysis(), m.setStatus("Removed the repo of "+login))
	}
	switch k.String() {
	case "K":
		return m.unlockSSH()
	case "a":
		return m.startClone(m.cloneSelection())
	case "A":
		if m.cloning {
			return nil
		}
		if v.confirmAll = len(m.classToClone()); v.confirmAll == 0 {
			return m.setStatus(m.nothingToClone())
		}
		return nil
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
	if v.confirm != "" || v.confirmAll > 0 {
		h -= 2
	}
	analysed := sDim.Render("code not analysed yet")
	switch {
	case m.analyzing:
		analysed = sAccent.Render(fmt.Sprintf("analysing code %d/%d…", m.anProg.done, m.anProg.total))
	case m.code != nil:
		analysed = sDim.Render(fmt.Sprintf("code analysed: %d repos, %d pairs sharing code (Code tab)", len(m.code.Repos), len(m.code.Pairs)))
	}
	v.locked, v.listed = m.sshLocked, len(m.list) > 0
	head := []string{
		sDim.Render(fmt.Sprintf("%d marked · cloned at the tag of their graded %s into %s", len(m.cloneSelection()), m.gradedSD(), m.repoRoot())),
		sDim.Render("A clones "+m.classLabel()+": shared code is only found between repos on disk. ") + analysed,
	}
	if m.cloneErr != "" {
		head = append(head, sBad.Render(m.cloneErr))
		h--
	}
	if m.sshLocked {
		head = append(head, sBad.Bold(true).Render("SSH refused your key: it is probably protected by a passphrase and no ssh-agent holds it. ")+
			keyHint("K", "unlock it (runs ssh-add)")+sDim.Render(", or run ssh-add before tpi."))
		h--
	}
	out := strings.Join(head, "\n") + "\n\n" + v.t.View(w, h-3)
	if v.confirm != "" {
		out += "\n\n" + sBad.Bold(true).Render("Remove "+data.RepoDir(m.loader.Cache.Root, m.ds.Activity.Slug, v.confirm)+"? ") + keyHint("y", "yes") + sDim.Render(" · any other key cancels")
	}
	if v.confirmAll > 0 {
		out += "\n\n" + sWarn.Bold(true).Render(fmt.Sprintf("Clone or update the repos of %s, %d students? ", m.classLabel(), v.confirmAll)) +
			keyHint("y", "yes") + sDim.Render(" · takes a while and disk space (tpi clean --repos frees it)")
	}
	return out
}
