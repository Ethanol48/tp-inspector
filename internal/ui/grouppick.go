package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tp-inspector/internal/cri"
)

// groupPickView is the F overlay: choose CRI classes, whose members become
// the group, as with --logins.
type groupPickView struct {
	picker *classPicker
	busy   string
	err    error
}

func (v *groupPickView) Title() string { return "Select group" }

// Capturing keeps esc and q for the picker: esc goes up one level.
func (v *groupPickView) Capturing() bool { return true }

func (v *groupPickView) Keys() []string {
	return append(v.picker.keys(), keyHint("x", "clear the group"))
}

func (v *groupPickView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if v.busy != "" || v.err != nil {
		if key == "esc" || key == "q" {
			m.popGroupPicker()
		}
		return nil
	}
	switch v.picker.handleKey(key) {
	case pickCancel:
		m.popGroupPicker()
	case pickClear:
		m.popGroupPicker()
		if len(m.list) == 0 {
			return nil
		}
		m.setGroup("", nil)
		return m.setStatus("Group cleared: whole promotion")
	case pickDone:
		cs := v.picker.chosen()
		v.busy = "Loading the members of " + classesLabel(v.picker.cat, cs) + "…"
		return loadMembers(m.CRI, v.picker.cat, cs)
	}
	if key == "q" && v.picker.level == pkCampus {
		m.popGroupPicker()
	}
	return nil
}

func (v *groupPickView) View(m *Model, w, h int) string {
	switch {
	case v.err != nil:
		return sBad.Render(v.err.Error()) + "\n\n" + sDim.Render("esc to go back")
	case v.busy != "":
		return sDim.Render(v.busy)
	}
	return v.picker.view(w, h)
}

// openGroupPicker shows the class picker, loading the classes first if needed.
func (m *Model) openGroupPicker() tea.Cmd {
	if m.CRI == nil {
		return m.setStatus("The CRI API is not available")
	}
	v := &groupPickView{picker: newClassPicker(cri.ActivityFilter(m.slug.Year, m.slug.Bimester))}
	m.push(v)
	if m.catalog != nil {
		v.picker.setCatalog(m.catalog)
		return nil
	}
	v.busy = "Loading the classes… (if a CRI login opens in your browser, log in there)"
	return loadCatalog(m.CRI)
}

func (m *Model) groupPicker() *groupPickView {
	for _, s := range m.stack {
		if v, ok := s.(*groupPickView); ok {
			return v
		}
	}
	return nil
}

func (m *Model) popGroupPicker() {
	for i, s := range m.stack {
		if _, ok := s.(*groupPickView); ok {
			m.stack = m.stack[:i]
			return
		}
	}
}

// handleGroupMsg handles the catalog and members loaded for the picker.
func (m *Model) handleGroupMsg(msg tea.Msg) tea.Cmd {
	v := m.groupPicker()
	switch msg := msg.(type) {
	case catalogMsg:
		if msg.err == nil {
			m.catalog = msg.cat
		}
		if v != nil {
			v.busy, v.err = "", msg.err
			if msg.err == nil {
				v.picker.setCatalog(msg.cat)
			}
		}
	case membersMsg:
		if v == nil {
			return nil
		}
		if msg.err != nil {
			v.busy, v.err = "", msg.err
			return nil
		}
		m.popGroupPicker()
		return m.setGroup(msg.label, msg.logins)
	}
	return nil
}

// setGroup replaces the --logins list by the given logins (none: the whole
// promotion) and focuses the views on them, as --logins does at start.
func (m *Model) setGroup(label string, logins []string) tea.Cmd {
	m.list = map[string]bool{}
	for _, l := range logins {
		m.list[l] = true
	}
	m.groupLabel = label
	on := len(logins) > 0
	for _, t := range m.tabs {
		switch t := t.(type) {
		case *overviewView:
			t.onlyList = on
		case *rankingView:
			t.onlyList = on
		}
	}
	m.gen++
	if !on {
		return nil
	}
	var missing []string
	for l := range m.list {
		if m.student(l) == nil {
			missing = append(missing, l)
		}
	}
	sort.Strings(missing)
	s := fmt.Sprintf("Group: %s, %d students", label, len(logins)-len(missing))
	if len(missing) > 0 {
		s += fmt.Sprintf(" (%d not in this assignment: %s)", len(missing), strings.Join(missing[:min(5, len(missing))], ", "))
		if len(missing) > 5 {
			s += "…"
		}
	}
	return m.setStatus(s)
}
