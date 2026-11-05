package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tp-inspector/internal/cri"
)

// Levels of the class picker.
const (
	pkCampus = iota
	pkPromo
	pkClass
)

// pickResult is what a key did in the picker.
type pickResult int

const (
	pickNone   pickResult = iota
	pickDone              // classes chosen: see chosen()
	pickCancel            // esc on the first level
	pickClear             // x: drop the current group
)

// classPicker chooses classes: campus, then promo (skipped when only one
// fits), then one or more classes (space marks, enter applies).
type classPicker struct {
	cat     *cri.Catalog
	filter  cri.Filter // classes that can take the activity
	all     bool       // a: ignore the filter
	level   int
	campus  string
	promo   string
	marked  map[string]bool
	skipped bool // the promo level was skipped on the way in
	tables  [3]Table
}

func newClassPicker(filter cri.Filter) *classPicker {
	p := &classPicker{filter: filter, marked: map[string]bool{}}
	p.tables[pkCampus].Cols = []Column{{Title: "Campus", Width: 0}, {Title: "Promos", Width: 6, Right: true}, {Title: "Classes", Width: 7, Right: true}}
	p.tables[pkPromo].Cols = []Column{{Title: "Promo", Width: 0}, {Title: "Semesters", Width: 14}, {Title: "Classes", Width: 7, Right: true}}
	p.tables[pkClass].Cols = []Column{{Title: "", Width: 3}, {Title: "Sem", Width: 4}, {Title: "Class", Width: 14}, {Title: "Name", Width: 0}, {Title: "Slug", Width: 34}}
	p.tables[pkCampus].Empty = "No class fits this activity (a shows every class)."
	return p
}

func (p *classPicker) setCatalog(cat *cri.Catalog) {
	p.cat = cat
	p.refresh()
}

// visible returns the classes the filter keeps.
func (p *classPicker) visible() []cri.Class {
	var out []cri.Class
	for _, c := range p.cat.Classes {
		if p.all || p.filter.Match(c) {
			out = append(out, c)
		}
	}
	return out
}

func (p *classPicker) inCampus() []cri.Class {
	var out []cri.Class
	for _, c := range p.visible() {
		if c.Campus == p.campus {
			out = append(out, c)
		}
	}
	return out
}

func (p *classPicker) inPromo() []cri.Class {
	var out []cri.Class
	for _, c := range p.inCampus() {
		if c.Promo == p.promo {
			out = append(out, c)
		}
	}
	return out
}

func promoName(promo string) string {
	if promo == "" {
		return "Other"
	}
	return promo
}

// promoOrder sorts promos by program (prepa, ING, apprentices, cyber), then
// by their first semester (SUP before SPE), then by name.
func promoOrder(cs []cri.Class) []string {
	first := map[string]int{}
	rank := map[string]int{}
	for _, c := range cs {
		if s, ok := first[c.Promo]; !ok || c.Semester < s {
			first[c.Promo] = c.Semester
		}
		rank[c.Promo] = cri.ProgramRank(c.Program)
	}
	out := make([]string, 0, len(first))
	for promo := range first {
		out = append(out, promo)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a == "") != (b == "") {
			return b == ""
		}
		if rank[a] != rank[b] {
			return rank[a] < rank[b]
		}
		if first[a] != first[b] {
			return first[a] < first[b]
		}
		return a < b
	})
	return out
}

// refresh rebuilds the rows of the current level.
func (p *classPicker) refresh() {
	if p.cat == nil {
		return
	}
	var rows []Row
	switch p.level {
	case pkCampus:
		promos := map[string]map[string]bool{}
		count := map[string]int{}
		var order []string
		for _, c := range p.visible() {
			if promos[c.Campus] == nil {
				promos[c.Campus] = map[string]bool{}
				order = append(order, c.Campus)
			}
			promos[c.Campus][c.Promo] = true
			count[c.Campus]++
		}
		sort.Slice(order, func(i, j int) bool {
			if (order[i] == "") != (order[j] == "") {
				return order[j] == ""
			}
			return p.cat.CampusName(order[i]) < p.cat.CampusName(order[j])
		})
		for _, cp := range order {
			rows = append(rows, Row{Key: cp, Cells: []string{p.cat.CampusName(cp), fmt.Sprint(len(promos[cp])), fmt.Sprint(count[cp])}})
		}
	case pkPromo:
		cs := p.inCampus()
		for _, promo := range promoOrder(cs) {
			// cs is sorted by semester, so labels come out in order.
			var labels []string
			seen := map[string]bool{}
			n := 0
			for _, c := range cs {
				if c.Promo == promo {
					if l := c.SemesterLabel(); l != "" && !seen[l] {
						seen[l] = true
						labels = append(labels, l)
					}
					n++
				}
			}
			rows = append(rows, Row{Key: promo, Cells: []string{promoName(promo), strings.Join(labels, ", "), fmt.Sprint(n)}})
		}
	case pkClass:
		for _, c := range p.inPromo() {
			box := sDim.Render("[ ]")
			if p.marked[c.Slug] {
				box = sAccent.Render("[x]")
			}
			name := c.Name
			if c.Private {
				name += sDim.Render(" (private)")
			}
			sem := c.SemesterLabel()
			if sem == "" {
				sem = sDim.Render("—")
			}
			rows = append(rows, Row{Key: c.Slug, Cells: []string{box, sem, sBold.Render(c.Code), name, sDim.Render(c.Slug)}})
		}
	}
	p.tables[p.level].SetRows(rows)
}

// open goes one level down from the row under the cursor.
func (p *classPicker) open() {
	sel := p.tables[p.level].Selected()
	if sel == nil {
		return
	}
	switch p.level {
	case pkCampus:
		p.campus = sel.Key
		p.level = pkPromo
		p.refresh()
		p.skipped = false
		if promos := promoOrder(p.inCampus()); len(promos) == 1 {
			p.promo = promos[0]
			p.level = pkClass
			p.skipped = true
			p.refresh()
		}
	case pkPromo:
		p.promo = sel.Key
		p.level = pkClass
		p.refresh()
	}
}

func (p *classPicker) back() bool {
	switch {
	case p.level == pkCampus:
		return false
	case p.level == pkClass && p.skipped:
		p.level = pkCampus
	default:
		p.level--
	}
	p.refresh()
	return true
}

// chosen returns the marked classes, or the one under the cursor.
func (p *classPicker) chosen() []cri.Class {
	var out []cri.Class
	for _, c := range p.cat.Classes {
		if p.marked[c.Slug] {
			out = append(out, c)
		}
	}
	if len(out) == 0 && p.level == pkClass {
		if sel := p.tables[pkClass].Selected(); sel != nil {
			for _, c := range p.cat.Classes {
				if c.Slug == sel.Key {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func (p *classPicker) handleKey(key string) pickResult {
	if p.cat == nil {
		if key == "esc" || key == "q" {
			return pickCancel
		}
		return pickNone
	}
	switch key {
	case "esc", "backspace", "left":
		if !p.back() && key == "esc" {
			return pickCancel
		}
	case "enter", "right":
		if p.level == pkClass {
			if key == "enter" && len(p.chosen()) > 0 {
				return pickDone
			}
			return pickNone
		}
		p.open()
	case " ":
		if p.level == pkClass {
			if sel := p.tables[pkClass].Selected(); sel != nil {
				p.marked[sel.Key] = !p.marked[sel.Key]
				p.tables[pkClass].HandleKey("down")
				p.refresh()
			}
		}
	case "a":
		if len(p.filter.Semesters) > 0 {
			p.all = !p.all
			p.level, p.campus, p.promo = pkCampus, "", ""
			p.refresh()
		}
	case "x":
		return pickClear
	default:
		p.tables[p.level].HandleKey(key)
	}
	return pickNone
}

// crumbs is the path shown in titles: Campus › Paris › SPE.
func (p *classPicker) crumbs() string {
	parts := []string{"Campus"}
	if p.level > pkCampus {
		parts = append(parts, p.cat.CampusName(p.campus))
	}
	if p.level > pkPromo {
		parts = append(parts, promoName(p.promo))
	}
	return strings.Join(parts, " › ")
}

func (p *classPicker) view(w, h int) string {
	var b strings.Builder
	line := sDim.Render(p.crumbs())
	switch {
	case len(p.filter.Semesters) == 0:
	case p.all:
		line += sDim.Render(" · every class (a: only ") + sAccent.Render(p.filter.Label()) + sDim.Render(")")
	default:
		line += sDim.Render(" · classes of ") + sAccent.Render(p.filter.Label()) + sDim.Render(" only (a: every class)")
	}
	marked := 0
	for _, on := range p.marked {
		if on {
			marked++
		}
	}
	if marked > 0 {
		line += sDim.Render(" · ") + sAccent.Render(fmt.Sprintf("%d marked", marked))
	}
	b.WriteString(line)
	b.WriteString("\n\n")
	b.WriteString(p.tables[p.level].View(w, max(3, h-2)))
	return b.String()
}

func (p *classPicker) keys() []string {
	hints := []string{keyHint("↑/↓", "move")}
	if p.level < pkClass {
		hints = append(hints, keyHint("enter", "open"))
	} else {
		hints = append(hints, keyHint("space", "mark"), keyHint("enter", "apply"))
	}
	if len(p.filter.Semesters) > 0 {
		hints = append(hints, keyHint("a", "every class / fitting"))
	}
	return hints
}

// classesLabel names chosen classes for titles: "SPE B1, B2 (Paris)".
func classesLabel(cat *cri.Catalog, cs []cri.Class) string {
	if len(cs) == 0 {
		return ""
	}
	codes := make([]string, len(cs))
	for i, c := range cs {
		codes[i] = c.Code
	}
	return fmt.Sprintf("%s %s (%s)", promoName(cs[0].Promo), strings.Join(codes, ", "), cat.CampusName(cs[0].Campus))
}

type catalogMsg struct {
	cat *cri.Catalog
	err error
}

func loadCatalog(src *cri.Source) tea.Cmd {
	return func() tea.Msg {
		cat, err := src.Catalog()
		return catalogMsg{cat, err}
	}
}

type membersMsg struct {
	label  string
	logins []string
	err    error
}

func loadMembers(src *cri.Source, cat *cri.Catalog, cs []cri.Class) tea.Cmd {
	slugs := make([]string, len(cs))
	for i, c := range cs {
		slugs[i] = c.Slug
	}
	label := classesLabel(cat, cs)
	return func() tea.Msg {
		ls, err := src.MembersOf(slugs)
		sort.Strings(ls)
		return membersMsg{label, ls, err}
	}
}

// GetGroupModel is `tpi get-group`: the picker on its own, which prints the
// members of the chosen classes.
type GetGroupModel struct {
	src      *cri.Source
	picker   *classPicker
	err      error
	busy     string
	printOut []string
	w, h     int
}

// RunGetGroup starts the picker and returns the logins of the chosen classes
// (nil when quitting). The interface is drawn on stderr so stdout stays clean.
func RunGetGroup(src *cri.Source, filter cri.Filter) ([]string, error) {
	m := &GetGroupModel{src: src, picker: newClassPicker(filter), busy: "Loading the classes…"}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr)).Run()
	if err == nil && m.err != nil && m.printOut == nil {
		err = m.err
	}
	return m.printOut, err
}

func (m *GetGroupModel) Init() tea.Cmd { return loadCatalog(m.src) }

func (m *GetGroupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case catalogMsg:
		m.busy, m.err = "", msg.err
		if msg.err == nil {
			m.picker.setCatalog(msg.cat)
		}
	case membersMsg:
		m.busy, m.err = "", msg.err
		if msg.err == nil {
			m.printOut = msg.logins
			return m, tea.Quit
		}
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" || key == "q" {
			return m, tea.Quit
		}
		if m.busy != "" {
			return m, nil
		}
		switch m.picker.handleKey(key) {
		case pickCancel:
			return m, tea.Quit
		case pickDone:
			cs := m.picker.chosen()
			m.busy = "Loading the members of " + classesLabel(m.picker.cat, cs) + "…"
			return m, loadMembers(m.src, m.picker.cat, cs)
		}
	}
	return m, nil
}

func (m *GetGroupModel) View() string {
	if m.w == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" " + sTitle.Render("tpi get-group") + "\n\n")
	h := max(3, m.h-5)
	var body string
	switch {
	case m.err != nil:
		body = sBad.Render(m.err.Error())
	case m.busy != "":
		body = sDim.Render(m.busy)
	default:
		body = m.picker.view(m.w-2, h)
	}
	for _, l := range strings.Split(body, "\n") {
		b.WriteString(" " + l + "\n")
	}
	b.WriteString(strings.Repeat("\n", max(0, h-strings.Count(body, "\n")-1)))
	hints := append(m.picker.keys(), keyHint("esc", "back"), keyHint("q", "quit"))
	b.WriteString("\n " + strings.Join(hints, sDim.Render(" · ")))
	return b.String()
}
