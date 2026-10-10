package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
)

type historyProgressMsg struct {
	login       string
	step        string
	done, total int
}

type historyLoadedMsg struct {
	login string
	h     *data.History
	err   error
}

var historySorts = []string{"activity", "date", "grade"}

// historyKinds are the activity kinds with their own average, in order.
var historyKinds = []struct{ kind, name string }{{"p", "TPs"}, {"e", "Exams"}, {"r", "Retakes"}}

// historySection is an academic year done with one class: a student who
// repeated a year has it twice, with two promos.
type historySection struct{ year, promo int }

func (s historySection) label() string { return fmt.Sprintf("Y%d · %d", s.year, s.promo) }

// academic returns the school year, e.g. 2025–26 for year 1 of the class of 2030.
func (s historySection) academic() string {
	start := s.promo - 5 + s.year - 1
	return fmt.Sprintf("%d–%02d", start, (start+1)%100)
}

func sectionOf(e data.HistoryEntry) (historySection, bool) {
	y, p, ok := e.Section()
	return historySection{y, p}, ok
}

// historyView shows the grade of one student in every activity of their
// class and the neighbouring ones (TPs, exams, retakes), one tab per
// academic year + class, downloaded on demand after a confirmation.
type historyView struct {
	login    string
	promo    int
	h        *data.History
	err      error
	confirm  bool // waiting for y to download
	loading  bool
	progress historyProgressMsg
	ch       chan tea.Msg

	sections []historySection // with at least one submission, oldest first
	sec      int              // 0 = summary, else sections[sec-1]
	tSum     Table
	t        Table
	built    *data.History
	sortMode int
	reverse  bool
	showAll  bool // include activities without any submission
	kind     int  // 0 = every kind, else historyKinds[kind-1]
}

func newHistoryView(m *Model, login string) *historyView {
	v := &historyView{login: login, promo: m.slug.Promo}
	if h, ok := m.loader.CachedHistory(login, v.promo); ok {
		v.h = h
	} else {
		v.confirm = true
	}
	return v
}

func (v *historyView) Title() string { return v.login + " · history" }

func (v *historyView) Keys() []string {
	switch {
	case v.confirm:
		return []string{keyHint("y", "download"), keyHint("n", "cancel")}
	case v.loading || v.h == nil:
		return nil
	}
	if v.sec == 0 {
		return []string{keyHint("enter", "open year"), keyHint("tab ←/→", "year"), keyHint("U", "download again")}
	}
	kind := "all"
	if v.kind > 0 {
		kind = historyKinds[v.kind-1].name
	}
	return []string{keyHint("tab ←/→", "year"), keyHint("s", "sort: "+historySorts[v.sortMode]), keyHint("r", "reverse"),
		keyHint("k", "kind: "+kind), keyHint("a", "without submission"), keyHint("enter", "open (this TP)"), keyHint("U", "download again")}
}

func (v *historyView) start(m *Model) tea.Cmd {
	v.confirm, v.loading, v.err = false, true, nil
	v.progress = historyProgressMsg{login: v.login, step: "Activities"}
	v.ch = make(chan tea.Msg, 64)
	ch, loader, login, promo := v.ch, m.loader, v.login, v.promo
	go func() {
		h, err := loader.LoadHistory(login, promo, func(step string, done, total int) {
			select {
			case ch <- historyProgressMsg{login, step, done, total}:
			default:
			}
		})
		ch <- historyLoadedMsg{login, h, err}
	}()
	return v.listen()
}

func (v *historyView) listen() tea.Cmd {
	ch := v.ch
	return func() tea.Msg { return <-ch }
}

// handle receives the background download messages routed by the model.
func (v *historyView) handle(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case historyProgressMsg:
		v.progress = msg
		return v.listen()
	case historyLoadedMsg:
		v.loading = false
		if msg.err != nil {
			v.err = msg.err
		} else {
			v.h = msg.h
		}
	}
	return nil
}

func (v *historyView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if v.confirm {
		if key == "y" {
			return v.start(m)
		}
		if v.h == nil {
			m.stack = m.stack[:len(m.stack)-1]
		}
		v.confirm = false
		return m.setStatus("Cancelled")
	}
	if v.loading {
		return nil
	}
	if v.h == nil {
		if key == "U" || key == "y" {
			return v.start(m)
		}
		return nil
	}
	if v.built != v.h {
		v.rebuild()
	}
	switch key {
	case "tab", "right", "l":
		v.sec = (v.sec + 1) % (len(v.sections) + 1)
		v.built = nil
		return nil
	case "shift+tab", "left", "h":
		v.sec = (v.sec + len(v.sections)) % (len(v.sections) + 1)
		v.built = nil
		return nil
	case "U":
		v.confirm = true
		return nil
	}
	if v.sec == 0 {
		if v.tSum.HandleKey(key) {
			return nil
		}
		if sel := v.tSum.Selected(); key == "enter" && sel != nil {
			for i, sc := range v.sections {
				if sc.label() == sel.Key {
					v.sec = i + 1
					v.built = nil
				}
			}
		}
		return nil
	}
	if v.t.HandleKey(key) {
		return nil
	}
	switch key {
	case "s":
		v.sortMode = (v.sortMode + 1) % len(historySorts)
	case "r":
		v.reverse = !v.reverse
	case "a":
		v.showAll = !v.showAll
	case "k":
		v.kind = (v.kind + 1) % (len(historyKinds) + 1)
	case "enter":
		if sel := v.t.Selected(); sel != nil {
			if sel.Key != m.ds.Activity.Slug {
				return m.setStatus("Only the open TP (" + m.ds.Activity.Slug + ") has traces here: run tpi " + sel.Key)
			}
			if m.student(v.login) != nil {
				m.push(newDetailView(v.login))
			}
		}
		return nil
	default:
		return nil
	}
	v.built = nil
	return nil
}

// kindOf returns the kind letter of an entry ("" when the slug is unknown).
func kindOf(e data.HistoryEntry) string {
	if s, ok := e.Slug(); ok {
		return s.Kind
	}
	return ""
}

// buildSections lists the academic year + class pairs with a submission.
func (v *historyView) buildSections() {
	seen := map[historySection]bool{}
	v.sections = nil
	for _, e := range v.h.Entries {
		if sc, ok := sectionOf(e); ok && e.Subs > 0 && !seen[sc] {
			seen[sc] = true
			v.sections = append(v.sections, sc)
		}
	}
	sort.Slice(v.sections, func(i, j int) bool {
		a, b := v.sections[i], v.sections[j]
		if a.academic() != b.academic() {
			return a.academic() < b.academic()
		}
		return a.year < b.year
	})
	v.sec = min(v.sec, len(v.sections))
}

// sectionEntries returns the entries of a section, or all of them for nil.
func (v *historyView) sectionEntries(sc *historySection) []data.HistoryEntry {
	if sc == nil {
		return v.h.Entries
	}
	var out []data.HistoryEntry
	for _, e := range v.h.Entries {
		if s, ok := sectionOf(e); ok && s == *sc {
			out = append(out, e)
		}
	}
	return out
}

func (v *historyView) current() *historySection {
	if v.sec == 0 {
		return nil
	}
	return &v.sections[v.sec-1]
}

func avgCell(s historyStat) string {
	if s.n == 0 {
		return sDim.Render("—")
	}
	return pctCell(s.sum / s.n)
}

func (v *historyView) rebuildSummary() {
	v.tSum.Cols = []Column{
		{Title: "School year", Width: 11},
		{Title: "Year", Width: 4},
		{Title: "Class", Width: 5},
		{Title: "TPs", Width: 5, Right: true},
		{Title: "Exams", Width: 5, Right: true},
		{Title: "Retakes", Width: 7, Right: true},
		{Title: "Overall", Width: 7, Right: true},
		{Title: "", Width: 12},
		{Title: "Graded", Width: 0},
	}
	years := map[int]int{}
	for _, sc := range v.sections {
		years[sc.year]++
	}
	var rows []Row
	for _, sc := range v.sections {
		st := historyStats(v.sectionEntries(&sc))
		all := st[len(st)-1]
		note := sDim.Render(fmt.Sprintf("%d / %d activities", all.n, len(v.sectionEntries(&sc))))
		if years[sc.year] > 1 {
			note += "  " + sWarn.Render(fmt.Sprintf("year %d done %d times", sc.year, years[sc.year]))
		}
		avg := -1
		if all.n > 0 {
			avg = all.sum / all.n
		}
		rows = append(rows, Row{Key: sc.label(), Cells: []string{
			sc.academic(), fmt.Sprint(sc.year), fmt.Sprint(sc.promo),
			avgCell(st[0]), avgCell(st[1]), avgCell(st[2]), avgCell(all), pctBar(avg, 12), note}})
	}
	v.tSum.Empty = "No submission in any activity of these classes."
	v.tSum.SetRows(rows)
}

func (v *historyView) rebuild() {
	v.buildSections()
	v.rebuildSummary()
	v.t.Cols = []Column{
		{Title: "Activity", Width: 22},
		{Title: "Kind", Width: 7},
		{Title: "Name", Width: 0},
		{Title: "Grade", Width: 5, Right: true},
		{Title: "", Width: 12},
		{Title: "Tag", Width: 18},
		{Title: "Submitted", Width: 14},
		{Title: "Subs", Width: 4, Right: true},
	}
	var es []data.HistoryEntry
	for _, e := range v.sectionEntries(v.current()) {
		if !v.showAll && e.Subs == 0 && e.Err == "" {
			continue
		}
		if v.kind > 0 && kindOf(e) != historyKinds[v.kind-1].kind {
			continue
		}
		es = append(es, e)
	}
	date := func(e data.HistoryEntry) time.Time {
		if e.Last == nil {
			return time.Time{}
		}
		return e.Last.ReceivedAt
	}
	order := func(e data.HistoryEntry) string {
		s, ok := e.Slug()
		if !ok {
			return "~" + e.Activity.Slug
		}
		return fmt.Sprintf("%d%02d-%02d-%s-%s", s.Year, s.Bimester, s.Number, s.Kind, e.Activity.Slug)
	}
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		var less bool
		switch v.sortMode {
		case 1:
			less = date(a).After(date(b))
		case 2:
			less = a.Percent() > b.Percent()
		default:
			less = order(a) < order(b)
		}
		if v.reverse {
			return !less
		}
		return less
	})
	rows := make([]Row, 0, len(es))
	for _, e := range es {
		name := e.Activity.Name
		if _, title, ok := strings.Cut(name, " - "); ok {
			name = title
		}
		kind := sDim.Render("?")
		if s, ok := e.Slug(); ok {
			kind = s.KindName()
		}
		slug := e.Activity.Slug
		var tag, when, subs string
		switch {
		case e.Err != "":
			tag = sBad.Render("error: " + e.Err)
		case e.Last == nil:
			tag = sDim.Render("no submission")
		default:
			tag, when = e.Last.Ref, e.Last.ReceivedAt.Local().Format("02/01/06 15:04")
			if e.Last.Status != "SUCCEEDED" {
				tag += " " + sBad.Render(strings.ToLower(orStatus(*e.Last)))
			}
		}
		if e.Subs > 0 {
			subs = fmt.Sprint(e.Subs)
		}
		rows = append(rows, Row{Key: e.Activity.Slug, Cells: []string{
			slug, kind, name, pctCell(e.Percent()), pctBar(e.Percent(), 12), tag, when, subs}})
	}
	v.t.Empty = "No submission in this year (a shows every activity)."
	v.t.SetRows(rows)
	v.built = v.h
}

// historyStat is the average grade of the activities of one kind.
type historyStat struct {
	name            string
	sum, n, total   int
	lowest, highest int
}

// historyStats returns one stat per historyKinds, then the overall one.
func historyStats(entries []data.HistoryEntry) []historyStat {
	all := historyStat{name: "Overall", lowest: 101, highest: -1}
	var out []historyStat
	add := func(s *historyStat, p int) {
		s.total++
		if p < 0 {
			return
		}
		s.sum += p
		s.n++
		s.lowest, s.highest = min(s.lowest, p), max(s.highest, p)
	}
	for _, k := range historyKinds {
		s := historyStat{name: k.name, lowest: 101, highest: -1}
		for _, e := range entries {
			if kindOf(e) == k.kind && e.Subs > 0 {
				add(&s, e.Percent())
			}
		}
		out = append(out, s)
	}
	for _, e := range entries {
		if e.Subs > 0 {
			add(&all, e.Percent())
		}
	}
	return append(out, all)
}

func (s historyStat) card() string {
	title := sSectionHdr.Render(s.name)
	if s.n == 0 {
		return sCard.Render(title + "\n" + sDim.Render("no grade") + "\n\n")
	}
	avg := s.sum / s.n
	big := pctStyle(avg).Bold(true).Render(fmt.Sprintf("%d%%", avg)) + sDim.Render(" average")
	return sCard.Render(title + "\n" + big + "\n" + pctBar(avg, 16) + "\n" +
		sDim.Render(fmt.Sprintf("%d graded · %d–%d%%", s.n, s.lowest, s.highest)))
}

func (v *historyView) View(m *Model, w, h int) string {
	switch {
	case v.confirm:
		est := ""
		var acts []forge.Activity
		promos := data.HistoryPromos(v.promo)
		if _, ok := (&data.Cache{Root: m.loader.Cache.Root}).ReadJSON("activities.json", &acts); ok {
			est = fmt.Sprintf(" (about %d)", len(data.ClassActivities(acts, promos)))
		}
		msg := sBold.Render("Download the history of "+v.login+"?") + "\n\n" +
			sDim.Render(fmt.Sprintf("One request per activity of the classes %d–%d%s: TPs, exams and retakes,", promos[0], promos[len(promos)-1], est)) + "\n" +
			sDim.Render("including the neighbouring classes to find a repeated year.") + "\n" +
			sDim.Render("Only grades are fetched, no traces. The result is cached.") + "\n\n" +
			keyHint("y", "download") + "   " + keyHint("n/esc", "cancel")
		if v.h != nil {
			msg += "\n\n" + sDim.Render("Current copy downloaded "+fmtDuration(time.Since(v.h.FetchedAt))+" ago.")
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, msg)
	case v.loading:
		p := v.progress
		line := sBold.Render("Downloading the history of "+v.login) + "\n" + sDim.Render(p.step)
		if p.total > 0 {
			line += sDim.Render(fmt.Sprintf("  %d/%d", p.done, p.total)) + "\n" +
				bar(float64(p.done)/float64(p.total), min(50, w-4), sAccent)
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, line)
	case v.h == nil:
		msg := "No history."
		if v.err != nil {
			msg = sBad.Render("Download failed: ") + v.err.Error()
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, msg+"\n\n"+keyHint("U", "retry"))
	}
	if v.built != v.h {
		v.rebuild()
	}
	withSubs, errs := 0, 0
	for _, e := range v.h.Entries {
		if e.Subs > 0 {
			withSubs++
		}
		if e.Err != "" {
			errs++
		}
	}
	info := fmt.Sprintf("Classes %d–%d · %d activities, %d with submissions · downloaded %s ago",
		v.h.Promos[0], v.h.Promos[len(v.h.Promos)-1], len(v.h.Entries), withSubs, fmtDuration(time.Since(v.h.FetchedAt)))
	if errs > 0 {
		info += sBad.Render(fmt.Sprintf(" · %d errors", errs))
	}

	names := []string{"Summary"}
	for _, sc := range v.sections {
		names = append(names, sc.label()+" "+sc.academic())
	}
	nav := renderTabs(names, v.sec)

	cur := v.current()
	var cards []string
	for _, st := range historyStats(v.sectionEntries(cur)) {
		cards = append(cards, st.card())
	}
	cardRow := lipgloss.JoinHorizontal(lipgloss.Top, cards...)
	if lipgloss.Width(cardRow) > w {
		cardRow = lipgloss.JoinHorizontal(lipgloss.Top, cards[:2]...) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, cards[2:]...)
	}
	title := "Every year"
	if cur != nil {
		title = fmt.Sprintf("Year %d with the class of %d (%s)", cur.year, cur.promo, cur.academic())
	}
	head := nav + "\n\n" + sBold.Render(title) + sDim.Render("  ·  "+info) + "\n" + cardRow + "\n"
	note := sDim.Render("Grade = newest submission of the graded definition (submit/submission); averages ignore activities without a submission.")
	if cur == nil {
		return head + "\n" + v.tSum.View(w, max(3, h-lipgloss.Height(head)-3)) + "\n" + note
	}
	return head + note + "\n\n" + v.t.View(w, max(3, h-lipgloss.Height(head)-2))
}
