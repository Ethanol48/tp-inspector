package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
)

var detailTabs = []string{"Trace", "Tags", "Suspects"}

// traceSD is the submission definition whose traces are browsed: the graded
// one ("submit" for TPs, "submission" for exams…); archi traces only check
// the repository layout.
func traceSD(m *Model) string { return m.ds.Graded }

// detailView is a student: the graded submit trace, every tag they pushed,
// then suspicious pairs.
type detailView struct {
	login  string
	sub    int
	gen    int
	tree   *traceTree // graded trace, nil if none
	treeOf string     // job id the tree was built for
	tTags  Table
	tSus   Table
	subs   map[string]forge.Submission // row key -> submission
	pairs  map[string][2]string        // row key -> {other, section}
}

func newDetailView(login string) *detailView { return &detailView{login: login, gen: -1} }

func (v *detailView) Title() string { return v.login }
func (v *detailView) Keys() []string {
	switch v.sub {
	case 0:
		k := []string{keyHint("tab", "section"), keyHint("H", "history")}
		if v.tree != nil {
			k = append(k, v.tree.keys()...)
		}
		return k
	case 1:
		return []string{keyHint("enter", "trace of a graded tag"), keyHint("tab", "section")}
	}
	return []string{keyHint("enter", "compare"), keyHint("tab", "section")}
}

// submitDF returns, for the graded traces of the promo, how many
// students fail each test and how many traces there are.
func submitDF(m *Model) (map[string]int, int) {
	df := map[string]int{}
	for _, s := range collectTestsFor(m, traceSD(m)) {
		df[s.id] = s.fail
	}
	return df, len(m.tracesFor(traceSD(m)))
}

// isGraded reports whether sub is the submission that counts for its definition.
func isGraded(st *data.Student, sub forge.Submission) bool {
	p := st.Picked[data.SDName(sub.SubmissionDefinitionURI)]
	return p != nil && p.SubmissionID == sub.ID
}

func (v *detailView) rebuild(m *Model) {
	st := m.student(v.login)
	if st == nil {
		return
	}
	// Keep the tree (and its folding) unless the graded job changed.
	if p := st.Picked[traceSD(m)]; p != nil && st.Traces[traceSD(m)] != nil {
		if v.treeOf != p.CurrentJobID {
			df, pop := submitDF(m)
			v.tree, v.treeOf = newTraceTree(st.Traces[traceSD(m)], df, pop), p.CurrentJobID
		}
	} else {
		v.tree, v.treeOf = nil, ""
	}

	v.tTags.Cols = []Column{{Title: "", Width: 1}, {Title: "Tag", Width: 16}, {Title: "Def.", Width: 7},
		{Title: "Date", Width: 11}, {Title: "Status", Width: 14}, {Title: "Grade", Width: 5, Right: true},
		{Title: "", Width: 12}, {Title: "Author", Width: 22}, {Title: "IP", Width: 0}}
	v.subs = map[string]forge.Submission{}
	var rows []Row
	for _, s := range st.Subs {
		v.subs[s.ID] = s
		state := sGood.Render("ok")
		if s.Status != "SUCCEEDED" {
			state = sBad.Render(strings.ToLower(orStatus(s)))
		}
		author := s.Author
		if author != "" && author != v.login {
			author = sBad.Bold(true).Render(author) // pushed by someone else
		}
		graded := " "
		if isGraded(st, s) {
			graded = sAccent.Render("★")
		}
		p := s.Percent()
		cells := []string{graded, s.Ref, sDim.Render(shortSD(s.SubmissionDefinitionURI)),
			fmtTime(s.ReceivedAt), state, pctCell(p), pctBar(p, 12), author, sDim.Render(s.IP)}
		if data.SDName(s.SubmissionDefinitionURI) != traceSD(m) {
			// archi tags only check the repository layout: listed, not browsable.
			for i := range cells {
				cells[i] = sDim.Render(ansiStrip(cells[i]))
			}
		}
		rows = append(rows, Row{Key: s.ID, Cells: cells})
	}
	v.tTags.Empty = "No submissions."
	v.tTags.SetRows(rows)

	v.tSus.Cols = []Column{{Title: "Signal", Width: 8}, {Title: "With", Width: 0}, {Title: "Score", Width: 5, Right: true}, {Title: "", Width: 12}}
	v.pairs = map[string][2]string{}
	rows = nil
	type sus struct {
		kind, other string
		score       float64
	}
	var ss []sus
	for _, p := range m.pairsTraces() {
		if p.A == v.login {
			ss = append(ss, sus{"traces", p.B, p.Score})
		} else if p.B == v.login {
			ss = append(ss, sus{"traces", p.A, p.Score})
		}
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].score > ss[j].score })
	for i, s := range ss {
		key := fmt.Sprint(i)
		v.pairs[key] = [2]string{s.other, s.kind}
		rows = append(rows, Row{Key: key, Cells: []string{sDim.Render(s.kind), s.other, scoreCell(s.score), bar(s.score, 12, sAccent)}})
	}
	v.tSus.Empty = "No suspicious pair with this student."
	v.tSus.SetRows(rows)
}

func orStatus(s forge.Submission) string {
	if s.Error != "" {
		return s.Error
	}
	return s.Status
}

func shortSD(uri string) string { return uri[strings.LastIndex(uri, "/")+1:] }

func (v *detailView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if key == "H" {
		m.openHistory(v.login)
		return nil
	}
	// In the trace tree ←/→ fold nodes, so only tab switches sections there.
	if v.sub == 0 && key != "tab" && key != "shift+tab" {
		if v.tree != nil {
			v.tree.handleKey(key)
		}
		return nil
	}
	switch key {
	case "tab", "right", "l":
		v.sub = (v.sub + 1) % len(detailTabs)
		return nil
	case "shift+tab", "left", "h":
		v.sub = (v.sub + len(detailTabs) - 1) % len(detailTabs)
		return nil
	}
	t := []*Table{nil, &v.tTags, &v.tSus}[v.sub]
	if t.HandleKey(key) {
		return nil
	}
	if key != "enter" {
		return nil
	}
	if v.sub == 1 {
		sel := v.tTags.Selected()
		if sel == nil {
			return nil
		}
		if data.SDName(v.subs[sel.Key].SubmissionDefinitionURI) != traceSD(m) {
			return m.setStatus("Traces can only be opened for tags of the graded definition (" + traceSD(m) + ")")
		}
		tv := newTagTraceView(v, sel.Key)
		m.push(tv)
		return tv.load(m)
	}
	if sel := v.tSus.Selected(); sel != nil {
		m.push(newCompareView(v.login, v.pairs[sel.Key][0]))
	}
	return nil
}

// studentHeader describes the student above every student view.
func studentHeader(m *Model, st *data.Student, w int) []string {
	rank, total := 0, 0
	p := st.Percent(m.sd)
	for _, o := range m.ds.Students {
		if op := o.Percent(m.sd); op >= 0 {
			total++
			if op > p {
				rank++
			}
		}
	}
	info := sBold.Render(st.Login) + "  " + pctCell(p) + sDim.Render(" ("+m.sd+")")
	if p >= 0 {
		info += sDim.Render(fmt.Sprintf("  · rank %d of %d", rank+1, total))
	}
	if len(st.Group.Members) > 1 {
		info += sDim.Render("  · group: " + strings.Join(st.Group.Members, ", "))
	}
	lines := []string{info}
	if st.Group.GitRemoteURL != "" {
		lines = append(lines, sDim.Render("git: "+st.Group.GitRemoteURL))
	}
	if dir := filepath.Join(m.repoRoot(), st.Login); dirExists(dir) {
		lines = append(lines, sDim.Render("local repo: "+dir))
	}
	for i := range lines {
		lines[i] = lipgloss.NewStyle().MaxWidth(w).Render(lines[i])
	}
	return lines
}

func (v *detailView) View(m *Model, w, h int) string {
	st := m.student(v.login)
	if st == nil {
		return sBad.Render("Unknown student: " + v.login)
	}
	if v.gen != m.gen {
		v.rebuild(m)
		v.gen = m.gen
	}
	lines := studentHeader(m, st, w)
	bodyH := h - len(lines) - 3
	var body string
	switch v.sub {
	case 0:
		last := st.Last(traceSD(m))
		switch {
		case v.tree == nil:
			body = "\n  " + sDim.Render("No graded trace ("+traceSD(m)+") for this student.")
		default:
			label := traceSD(m)
			if last != nil {
				label = fmt.Sprintf("%s · %s · graded", last.Ref, fmtTime(last.ReceivedAt))
			}
			pct := -1
			if last != nil {
				pct = last.Percent()
			}
			body = sDim.Render(label) + "\n" + v.tree.View(pct, w, bodyH-1)
		}
	case 1:
		body = v.tTags.View(w, bodyH-1) + "\n" + sDim.Render("  ★ graded submission · enter opens the trace of a "+traceSD(m)+" tag (dimmed tags, e.g. archi, are not browsable)")
	default:
		body = v.tSus.View(w, bodyH)
	}
	return strings.Join(lines, "\n") + "\n\n" + renderTabs(detailTabs, v.sub) + "\n" + body
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// tagTraceView shows the trace of one tag as a tree; [ and ] move between tags.
type tagTraceView struct {
	parent *detailView
	subID  string
	tree   *traceTree
	treeOf string // job id the tree was built for
}

func newTagTraceView(parent *detailView, subID string) *tagTraceView {
	return &tagTraceView{parent: parent, subID: subID}
}

func (v *tagTraceView) submission() forge.Submission { return v.parent.subs[v.subID] }

func (v *tagTraceView) Title() string { return v.submission().Ref }

func (v *tagTraceView) Keys() []string {
	k := []string{keyHint("[/]", "older/newer tag")}
	if v.tree != nil {
		k = append(k, v.tree.keys()...)
	}
	return k
}

func (v *tagTraceView) load(m *Model) tea.Cmd { return m.fetchJobTrace(v.submission()) }

// step moves to the older (+1) or newer (-1) submit tag of the list.
// Only that one trace is fetched, and only if it is not cached.
func (v *tagTraceView) step(m *Model, d int) tea.Cmd {
	rows := v.parent.tTags.Rows
	for i, r := range rows {
		if r.Key != v.subID {
			continue
		}
		for j := i + d; j >= 0 && j < len(rows); j += d {
			if data.SDName(v.parent.subs[rows[j].Key].SubmissionDefinitionURI) == traceSD(m) {
				v.subID = rows[j].Key
				v.parent.tTags.cursor = j
				v.tree, v.treeOf = nil, ""
				return v.load(m)
			}
		}
		break
	}
	return nil
}

func (v *tagTraceView) Update(m *Model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "[":
		return v.step(m, 1)
	case "]":
		return v.step(m, -1)
	}
	if v.tree != nil {
		v.tree.handleKey(k.String())
	}
	return nil
}

func (v *tagTraceView) View(m *Model, w, h int) string {
	st := m.student(v.parent.login)
	sub := v.submission()
	lines := studentHeader(m, st, w)
	graded := ""
	if isGraded(st, sub) {
		graded = sAccent.Render("  ★ graded")
	}
	lines = append(lines, "", sBold.Render(sub.Ref)+sDim.Render(fmt.Sprintf("  · %s · %s · %s",
		shortSD(sub.SubmissionDefinitionURI), fmtTime(sub.ReceivedAt), strings.ToLower(orStatus(sub))))+graded)
	head := strings.Join(lines, "\n")
	bodyH := h - len(lines) - 1

	var body string
	switch {
	case sub.CurrentJob == nil:
		body = sDim.Render("This submission has no test job (" + strings.ToLower(orStatus(sub)) + ").")
	case m.jobErrs[sub.CurrentJob.ID] != nil:
		body = sBad.Render("Could not load the trace: " + m.jobErrs[sub.CurrentJob.ID].Error())
	case m.jobTraces[sub.CurrentJob.ID] == nil:
		body = sDim.Render("Loading trace…")
	default:
		if v.treeOf != sub.CurrentJob.ID {
			sd := data.SDName(sub.SubmissionDefinitionURI)
			df := map[string]int{}
			for _, s := range collectTestsFor(m, sd) {
				df[s.id] = s.fail
			}
			v.tree = newTraceTree(m.jobTraces[sub.CurrentJob.ID], df, len(m.tracesFor(sd)))
			v.treeOf = sub.CurrentJob.ID
		}
		body = v.tree.View(sub.Percent(), w, bodyH)
	}
	return head + "\n" + body
}

// ansiStrip removes styling so a row can be dimmed as a whole.
func ansiStrip(s string) string { return ansi.Strip(s) }
