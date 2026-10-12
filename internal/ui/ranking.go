package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"tp-inspector/internal/data"
)

var rankingSorts = []string{"grade", "submission time", "login", "submissions", "suspicion"}

// rankingColumn is a column of the Ranking tab; optional ones can be hidden.
type rankingColumn struct {
	id       string
	col      Column
	optional bool
	desc     string
}

var rankingColumns = []rankingColumn{
	{id: "rank", col: Column{Title: "#", Width: 4, Right: true}, optional: true, desc: "position in the current order"},
	{id: "mark", col: Column{Title: "", Width: 1}},
	{id: "login", col: Column{Title: "Login", Width: 0}},
	{id: "grade", col: Column{Title: "Grade", Width: 5, Right: true}, optional: true, desc: "grade of the graded submission"},
	{id: "bar", col: Column{Title: "", Width: 12}, optional: true, desc: "grade bar"},
	{id: "tag", col: Column{Title: "Tag", Width: 16}, optional: true, desc: "tag of the graded submission"},
	{id: "date", col: Column{Title: "Submitted", Width: 11}, optional: true, desc: "time of the graded submission"},
	{id: "subs", col: Column{Title: "Subs", Width: 6, Right: true}, optional: true, desc: "number of submissions"},
	{id: "status", col: Column{Title: "Status", Width: 12}, optional: true, desc: "status of the last submission"},
	{id: "ip", col: Column{Title: "IP", Width: 15}, optional: true, desc: "IP of the graded submission"},
	{id: "traces", col: Column{Title: "Suspicion", Width: 9}, optional: true, desc: "highest suspicion level (high / medium / low) of their pairs"},
}

// defaultHidden are the optional columns hidden until the user shows them.
var defaultHidden = map[string]bool{"ip": true}

type rankingView struct {
	hidden    map[string]bool
	t         Table
	sortMode  int
	reverse   bool
	filter    textinput.Model
	filtering bool
	onlyList  bool // show only the --logins list and marked students
	gen       int
}

func newRankingView() *rankingView {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "login…"
	v := &rankingView{filter: ti, gen: -1, hidden: map[string]bool{}}
	if saved, ok := loadConfig().HiddenColumns["ranking"]; ok {
		for _, id := range saved {
			v.hidden[id] = true
		}
	} else {
		for id := range defaultHidden {
			v.hidden[id] = true
		}
	}
	return v
}

// saveColumns persists the hidden columns.
func (v *rankingView) saveColumns() error {
	c := loadConfig()
	ids := []string{}
	for _, rc := range rankingColumns {
		if v.hidden[rc.id] {
			ids = append(ids, rc.id)
		}
	}
	c.HiddenColumns["ranking"] = ids
	return c.save()
}

func (v *rankingView) Title() string   { return "Ranking" }
func (v *rankingView) Capturing() bool { return v.filtering }

func (v *rankingView) Keys() []string {
	if v.filtering {
		return []string{keyHint("enter", "apply"), keyHint("esc", "clear filter")}
	}
	return []string{keyHint("enter", "trace"), keyHint("s", "sort: "+rankingSorts[v.sortMode]), keyHint("r", "reverse"),
		keyHint("/", "filter"), keyHint("c", "compare"), keyHint("H", "history"), keyHint("f", "selection"), keyHint("v", "columns")}
}

func statusCell(last *statusInfo) string {
	switch {
	case last == nil:
		return sDim.Render("no submission")
	case last.status == "SUCCEEDED":
		return sGood.Render("ok")
	case last.status == "PROCESSING" || last.status == "IDLE":
		return sWarn.Render("running")
	case last.err != "":
		return sBad.Render(strings.ToLower(last.err))
	default:
		return sBad.Render(strings.ToLower(last.status))
	}
}

type statusInfo struct{ status, err string }

func (v *rankingView) rebuild(m *Model) {
	type entry struct {
		st  *data.Student
		pct int
		n   int
		tr  float64
	}
	q := strings.ToLower(strings.TrimSpace(v.filter.Value()))
	var es []entry
	for _, st := range m.ds.Students {
		if q != "" && !strings.Contains(strings.ToLower(st.Login), q) {
			continue
		}
		if v.onlyList && !m.list[st.Login] && !m.marked[st.Login] {
			continue
		}
		es = append(es, entry{st, st.Percent(m.sd), len(st.SubsFor(m.sd)), m.suspicionOf(st.Login)})
	}
	less := func(a, b entry) bool {
		la, lb := a.st.Last(m.sd), b.st.Last(m.sd)
		switch v.sortMode {
		case 0:
			if a.pct != b.pct {
				return a.pct > b.pct
			}
		case 1:
			switch {
			case la == nil && lb != nil:
				return false
			case la != nil && lb == nil:
				return true
			case la != nil && lb != nil && !la.ReceivedAt.Equal(lb.ReceivedAt):
				return la.ReceivedAt.Before(lb.ReceivedAt)
			}
		case 3:
			if a.n != b.n {
				return a.n > b.n
			}
		case 4:
			if a.tr != b.tr {
				return a.tr > b.tr
			}
		}
		return a.st.Login < b.st.Login
	}
	sort.SliceStable(es, func(i, j int) bool {
		if v.reverse {
			return less(es[j], es[i])
		}
		return less(es[i], es[j])
	})
	rows := make([]Row, len(es))
	for i, e := range es {
		last := e.st.Last(m.sd)
		var si *statusInfo
		tag, date := sDim.Render("—"), sDim.Render("—")
		if last != nil {
			si = &statusInfo{last.Status, last.Error}
			tag, date = last.Ref, fmtTime(last.ReceivedAt)
		}
		mark := " "
		switch {
		case m.marked[e.st.Login]:
			mark = sAccent.Render("●")
		case m.list[e.st.Login]:
			mark = sDim.Render("◦")
		}
		ip := sDim.Render("—")
		if last != nil && last.IP != "" {
			ip = sDim.Render(last.IP)
		}
		cells := map[string]string{
			"rank": sDim.Render(fmt.Sprint(i + 1)), "mark": mark, "login": e.st.Login,
			"grade": pctCell(e.pct), "bar": pctBar(e.pct, 12), "tag": tag, "date": date,
			"subs": fmt.Sprint(e.n), "status": statusCell(si), "ip": ip,
			"traces": levelCell(e.tr),
		}
		row := Row{Key: e.st.Login}
		for _, rc := range rankingColumns {
			if !v.hidden[rc.id] {
				row.Cells = append(row.Cells, cells[rc.id])
			}
		}
		rows[i] = row
	}
	v.t.Cols = nil
	for _, rc := range rankingColumns {
		if !v.hidden[rc.id] {
			v.t.Cols = append(v.t.Cols, rc.col)
		}
	}
	v.t.Empty = "No student matches the filter."
	if v.onlyList && len(m.list) == 0 && len(m.marked) == 0 {
		v.t.Empty = "No selection: use --logins list.txt or mark students with space (f to show everyone)."
	}
	v.t.SetRows(rows)
}

func (v *rankingView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	if v.filtering {
		switch k.String() {
		case "enter":
			v.filtering = false
			v.filter.Blur()
		case "esc":
			v.filtering = false
			v.filter.Blur()
			v.filter.SetValue("")
		default:
			var cmd tea.Cmd
			v.filter, cmd = v.filter.Update(k)
			v.gen = -1
			return cmd
		}
		v.gen = -1
		return nil
	}
	if v.t.HandleKey(k.String()) {
		return nil
	}
	sel := v.t.Selected()
	switch k.String() {
	case "s":
		v.sortMode = (v.sortMode + 1) % len(rankingSorts)
		v.gen = -1
	case "r":
		v.reverse = !v.reverse
		v.gen = -1
	case "f":
		v.onlyList = !v.onlyList
		v.gen = -1
	case "v":
		m.push(&columnsView{rv: v})
	case "/":
		v.filtering = true
		return v.filter.Focus()
	case " ":
		if sel != nil {
			m.marked[sel.Key] = !m.marked[sel.Key]
			m.gen++ // the group changed
			v.gen = -1
			v.t.HandleKey("down")
		}
	case "c":
		if sel != nil {
			return m.compareMarked(sel.Key)
		}
		return m.compareMarked("")
	case "H":
		if sel != nil {
			m.openHistory(sel.Key)
		}
	case "enter":
		if sel != nil {
			m.push(newDetailView(sel.Key))
		}
	}
	return nil
}

func (v *rankingView) View(m *Model, w, h int) string {
	if v.gen != m.gen {
		v.rebuild(m)
		v.gen = m.gen
	}
	top := ""
	if v.filtering || v.filter.Value() != "" {
		top = v.filter.View() + "\n"
		h--
	}
	if v.onlyList {
		top += sDim.Render(fmt.Sprintf("Selection only: %d from the list + %d marked by hand (f to show everyone)", len(v.t.Rows)-countTrue(m.marked), countTrue(m.marked))) + "\n"
		h--
	}
	return top + v.t.View(w, h)
}

func countTrue(m map[string]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}

// columnsView lets the user show or hide the optional Ranking columns.
type columnsView struct {
	rv     *rankingView
	cursor int
}

func (c *columnsView) Title() string { return "Columns" }
func (c *columnsView) Keys() []string {
	return []string{keyHint("space/enter", "show/hide"), keyHint("a", "show all")}
}

func optionalColumns() []rankingColumn {
	var out []rankingColumn
	for _, rc := range rankingColumns {
		if rc.optional {
			out = append(out, rc)
		}
	}
	return out
}

func (c *columnsView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	cols := optionalColumns()
	switch k.String() {
	case "up", "k":
		c.cursor = max(0, c.cursor-1)
	case "down", "j":
		c.cursor = min(len(cols)-1, c.cursor+1)
	case " ", "enter", "x":
		id := cols[c.cursor].id
		c.rv.hidden[id] = !c.rv.hidden[id]
	case "a":
		c.rv.hidden = map[string]bool{}
	default:
		return nil
	}
	c.rv.gen = -1
	if err := c.rv.saveColumns(); err != nil {
		return m.setStatus("Could not save columns: " + err.Error())
	}
	return nil
}

func (c *columnsView) View(m *Model, w, h int) string {
	lines := []string{sDim.Render("Ranking columns (saved in " + configPath() + ")"), ""}
	for i, rc := range optionalColumns() {
		prefix := "  "
		if i == c.cursor {
			prefix = sAccent.Bold(true).Render("❯ ")
		}
		box := sGood.Render("[x]")
		if c.rv.hidden[rc.id] {
			box = sDim.Render("[ ]")
		}
		title := rc.col.Title
		if title == "" {
			title = rc.id
		}
		lines = append(lines, fmt.Sprintf("%s%s %-10s %s", prefix, box, title, sDim.Render(rc.desc)))
	}
	return strings.Join(lines, "\n")
}
