// Package ui is the Bubble Tea interface of tpi.
package ui

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tp-inspector/internal/analysis"
	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
	"tp-inspector/internal/repos"
	"tp-inspector/internal/tp"
	"tp-inspector/internal/trace"
)

// view is a tab or an overlay pushed on top of the tabs.
type view interface {
	Title() string
	Update(m *Model, k tea.KeyMsg) tea.Cmd
	View(m *Model, w, h int) string
	Keys() []string
}

// capturer is implemented by views with a focused text input.
type capturer interface{ Capturing() bool }

type progressMsg struct {
	step        string
	done, total int
}

type loadedMsg struct {
	ds  *data.Dataset
	err error
}

type cloneProgressMsg struct{ done, total int }

type cloneDoneMsg struct{ results []repos.Result }

type statusClearMsg struct{ id int }

// Model is the root Bubble Tea model.
type Model struct {
	slug   tp.Slug
	loader *data.Loader

	ds     *data.Dataset
	err    error
	sd     string
	gen    int // bumped whenever derived data must be rebuilt
	tabs   []view
	active int
	stack  []view
	w, h   int

	loading  bool
	progress progressMsg
	events   chan tea.Msg

	marked map[string]bool // selected by hand with space in Ranking
	list   map[string]bool // logins from --logins

	tracePairs map[string][]analysis.TracePair
	suspicion  map[string]map[string][2]float64 // sd -> login -> {traces, code}

	clones    map[string]repos.Result // login -> last clone result
	cloning   bool
	cloneProg cloneProgressMsg

	status   string
	statusID int

	jobTraces map[string]*trace.Trace // job id -> trace, for any tag
	jobErrs   map[string]error
	jobBusy   map[string]bool
}

type jobTraceMsg struct {
	jobID string
	tr    *trace.Trace
	err   error
}

// fetchJobTrace loads the trace of any submission in the background.
func (m *Model) fetchJobTrace(sub forge.Submission) tea.Cmd {
	if sub.CurrentJob == nil {
		return nil
	}
	id := sub.CurrentJob.ID
	if m.jobTraces[id] != nil || m.jobErrs[id] != nil || m.jobBusy[id] {
		return nil
	}
	m.jobBusy[id] = true
	ds, loader := m.ds, m.loader
	return func() tea.Msg {
		tr, err := loader.JobTrace(ds, sub)
		return jobTraceMsg{id, tr, err}
	}
}

// New creates the model; call Run to start it.
func New(slug tp.Slug, loader *data.Loader, list []string) *Model {
	m := &Model{slug: slug, loader: loader, marked: map[string]bool{}, list: map[string]bool{},
		tracePairs: map[string][]analysis.TracePair{}, suspicion: map[string]map[string][2]float64{},
		clones:    map[string]repos.Result{},
		jobTraces: map[string]*trace.Trace{}, jobErrs: map[string]error{}, jobBusy: map[string]bool{}}
	for _, l := range list {
		m.list[l] = true
	}
	rv := newRankingView()
	rv.onlyList = len(list) > 0
	m.tabs = []view{&overviewView{}, rv, &testsView{}, &tracesView{}, &reposView{}}
	return m
}

// Run starts the program.
func Run(m *Model) error {
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *Model) Init() tea.Cmd { return m.startLoad() }

func (m *Model) listen() tea.Cmd {
	ch := m.events
	return func() tea.Msg { return <-ch }
}

func (m *Model) startLoad() tea.Cmd {
	m.loading = true
	m.progress = progressMsg{step: "Connecting"}
	m.events = make(chan tea.Msg, 64)
	ch := m.events
	go func() {
		ds, err := m.loader.Load(m.slug, func(step string, done, total int) {
			select {
			case ch <- progressMsg{step, done, total}:
			default: // drop progress updates rather than block the loader
			}
		})
		ch <- loadedMsg{ds, err}
	}()
	return m.listen()
}

func (m *Model) setStatus(s string) tea.Cmd {
	m.status = s
	m.statusID++
	id := m.statusID
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return statusClearMsg{id} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case statusClearMsg:
		if msg.id == m.statusID {
			m.status = ""
		}
		return m, nil
	case progressMsg:
		m.progress = msg
		return m, m.listen()
	case loadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.ds = msg.ds
			if m.sd == "" || m.ds.SDURIs[m.sd] == "" {
				m.sd = m.ds.SDs[0]
			}
			m.tracePairs = map[string][]analysis.TracePair{}
			m.suspicion = map[string]map[string][2]float64{}
			m.stack = nil
			m.gen++
			var unknown []string
			for l := range m.list {
				if m.student(l) == nil {
					unknown = append(unknown, l)
				}
			}
			if len(unknown) > 0 {
				sort.Strings(unknown)
				return m, m.setStatus(fmt.Sprintf("%d logins from the list are not in this assignment: %s", len(unknown), strings.Join(unknown, ", ")))
			}
			if n := len(m.ds.Warnings); n > 0 {
				return m, m.setStatus(fmt.Sprintf("%d warnings while loading (e.g. %s)", n, m.ds.Warnings[0]))
			}
		}
		return m, nil
	case jobTraceMsg:
		delete(m.jobBusy, msg.jobID)
		if msg.err != nil {
			m.jobErrs[msg.jobID] = msg.err
		} else {
			m.jobTraces[msg.jobID] = msg.tr
		}
		m.gen++
		return m, nil
	case historyProgressMsg:
		return m, m.routeHistory(msg.login, msg)
	case historyLoadedMsg:
		return m, m.routeHistory(msg.login, msg)
	case cloneProgressMsg:
		m.cloneProg = msg
		return m, m.listen()
	case cloneDoneMsg:
		m.cloning = false
		failed := 0
		for _, r := range msg.results {
			m.clones[r.Login] = r
			if r.Err != nil {
				failed++
			}
		}
		m.gen++
		if failed > 0 {
			return m, m.setStatus(fmt.Sprintf("Cloned %d repos, %d failed", len(msg.results)-failed, failed))
		}
		return m, m.setStatus(fmt.Sprintf("Cloned %d repos into %s", len(msg.results), m.repoRoot()))
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) top() view {
	if len(m.stack) > 0 {
		return m.stack[len(m.stack)-1]
	}
	return m.tabs[m.active]
}

func (m *Model) push(v view) { m.stack = append(m.stack, v) }

// routeHistory hands a history download message to the open history view of
// that student; the download goes on (and is cached) if it was closed.
func (m *Model) routeHistory(login string, msg tea.Msg) tea.Cmd {
	for _, v := range m.stack {
		if hv, ok := v.(*historyView); ok && hv.login == login {
			return hv.handle(msg)
		}
	}
	return nil
}

// openHistory shows the history of a student, asking before downloading it.
func (m *Model) openHistory(login string) {
	m.push(newHistoryView(m, login))
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if m.loading || m.ds == nil {
		if key == "q" || key == "esc" {
			return tea.Quit
		}
		if key == "R" && !m.loading {
			return m.startLoad()
		}
		return nil
	}
	top := m.top()
	if c, ok := top.(capturer); ok && c.Capturing() {
		return top.Update(m, k)
	}
	switch key {
	case "q", "esc":
		if len(m.stack) > 0 {
			m.stack = m.stack[:len(m.stack)-1]
			return nil
		}
		if key == "q" {
			return tea.Quit
		}
		return nil
	case "?":
		if _, ok := top.(*helpView); ok {
			m.stack = m.stack[:len(m.stack)-1]
		} else {
			m.push(&helpView{})
		}
		return nil
	case "m":
		if len(m.ds.SDs) > 1 {
			for i, sd := range m.ds.SDs {
				if sd == m.sd {
					m.sd = m.ds.SDs[(i+1)%len(m.ds.SDs)]
					break
				}
			}
			m.gen++
			return m.setStatus("Submission definition: " + m.sd)
		}
		return nil
	case "R":
		if m.cloning {
			return m.setStatus("Wait for the clone to finish")
		}
		m.loader.Cache.Refresh = true
		return m.startLoad()
	case "e":
		p, err := m.export()
		if err != nil {
			return m.setStatus("Export failed: " + err.Error())
		}
		return m.setStatus("Exported to " + p)
	}
	if len(m.stack) == 0 {
		switch key {
		case "tab", "right", "l":
			m.active = (m.active + 1) % len(m.tabs)
			return nil
		case "shift+tab", "left", "h":
			m.active = (m.active + len(m.tabs) - 1) % len(m.tabs)
			return nil
		case "1", "2", "3", "4", "5":
			if i := int(key[0] - '1'); i < len(m.tabs) {
				m.active = i
			}
			return nil
		}
	}
	return top.Update(m, k)
}

func (m *Model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.err != nil {
		return lipgloss.NewStyle().Padding(1, 2).Render(
			sBad.Bold(true).Render("Error") + "\n\n" + m.err.Error() + "\n\n" +
				keyHint("R", "retry") + "   " + keyHint("q", "quit"))
	}
	if m.loading || m.ds == nil {
		return m.loadingView()
	}

	// Header: title + tabs (or breadcrumb when an overlay is open).
	name := m.ds.Activity.Name
	if name == "" {
		name = m.slug.Raw
	}
	right := sDim.Render(fmt.Sprintf("%s · data from %s ago", m.sd, fmtDuration(time.Since(m.ds.FetchedAt))))
	left := sTitle.Render("tpi") + sDim.Render(" · ") + sBold.Render(name)
	header := left + strings.Repeat(" ", max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right)-2)) + right

	names := make([]string, len(m.tabs))
	for i, t := range m.tabs {
		names[i] = t.Title()
	}
	nav := renderTabs(names, m.active)
	if len(m.stack) > 0 {
		crumbs := []string{sDim.Render(m.tabs[m.active].Title())}
		for i, v := range m.stack {
			if i == len(m.stack)-1 {
				crumbs = append(crumbs, sTabActive.Render(v.Title()))
			} else {
				crumbs = append(crumbs, sDim.Render(v.Title()))
			}
		}
		nav = strings.Join(crumbs, sDim.Render(" › "))
	}

	top := m.top()
	footer := m.footer(top)
	if m.status != "" {
		footer = sAccent.Render(m.status)
	}

	bodyH := max(3, m.h-5)
	body := top.View(m, m.w-2, bodyH)
	body = lipgloss.NewStyle().MaxHeight(bodyH).MaxWidth(m.w - 2).Render(body)
	bodyLines := strings.Count(body, "\n") + 1

	var b strings.Builder
	b.WriteString(" " + header + "\n")
	b.WriteString(" " + nav + "\n\n")
	for _, l := range strings.Split(body, "\n") {
		b.WriteString(" " + l + "\n")
	}
	b.WriteString(strings.Repeat("\n", max(0, bodyH-bodyLines)))
	b.WriteString(" " + lipgloss.NewStyle().MaxWidth(m.w-2).Render(footer))
	return b.String()
}

// footer fits the key hints to the terminal width: the view's own hints come
// first and are dropped from the end when space runs out, while the closing
// hints (help, back/quit) always stay.
func (m *Model) footer(top view) string {
	sep := sDim.Render(" · ")
	optional := append(top.Keys(), keyHint("m", "submit/archi"), keyHint("e", "export"))
	tail := []string{keyHint("?", "help")}
	if len(m.stack) > 0 {
		tail = append(tail, keyHint("esc", "back"))
	} else {
		tail = append(tail, keyHint("q", "quit"))
	}
	avail := m.w - 2 - lipgloss.Width(strings.Join(tail, sep)) - lipgloss.Width(sep)
	var kept []string
	used := 0
	for _, h := range optional {
		w := lipgloss.Width(h)
		if len(kept) > 0 {
			w += lipgloss.Width(sep)
		}
		if used+w > avail {
			break
		}
		kept = append(kept, h)
		used += w
	}
	return strings.Join(append(kept, tail...), sep)
}

func (m *Model) loadingView() string {
	p := m.progress
	line := p.step
	var barLine string
	if p.total > 0 {
		line += fmt.Sprintf("  %d/%d", p.done, p.total)
		barLine = bar(float64(p.done)/float64(p.total), min(50, m.w-8), sAccent)
	}
	content := sTitle.Render("tpi") + sDim.Render(" · "+m.slug.Describe()) + "\n\n" +
		sBold.Render("Loading ") + line + "\n" + barLine
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, content)
}

// ---- derived data -------------------------------------------------------

func (m *Model) traces() map[string]*trace.Trace { return m.tracesFor(m.sd) }

func (m *Model) tracesFor(sd string) map[string]*trace.Trace {
	out := map[string]*trace.Trace{}
	for _, st := range m.ds.Students {
		if t := st.Traces[sd]; t != nil && len(t.Tests) > 0 {
			out[st.Login] = t
		}
	}
	return out
}

func (m *Model) student(login string) *data.Student {
	for _, st := range m.ds.Students {
		if st.Login == login {
			return st
		}
	}
	return nil
}

func (m *Model) pairsTraces() []analysis.TracePair {
	if p, ok := m.tracePairs[m.sd]; ok {
		return p
	}
	p := analysis.CompareTraces(m.traces(), analysis.TraceOptions{})
	m.tracePairs[m.sd] = p
	return p
}

// suspicionOf returns the best trace similarity score of pairs involving login.
func (m *Model) suspicionOf(login string) float64 {
	s, ok := m.suspicion[m.sd]
	if !ok {
		s = map[string][2]float64{}
		for _, p := range m.pairsTraces() {
			for _, l := range []string{p.A, p.B} {
				if cur := s[l]; p.Score > cur[0] {
					s[l] = [2]float64{p.Score, 0}
				}
			}
		}
		m.suspicion[m.sd] = s
	}
	return s[login][0]
}

func (m *Model) repoRoot() string {
	return filepath.Join(m.loader.Cache.Root, m.ds.Activity.Slug, "repos")
}

// cloneSelection returns the students marked by hand in Ranking: the only
// repos ever cloned (never the whole class, nor the whole --logins list).
func (m *Model) cloneSelection() []string {
	var out []string
	for l, ok := range m.marked {
		if ok && (m.ds == nil || m.student(l) != nil) {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

// repoLogins lists the marked students plus every repo already on disk,
// reading the state of clones made in previous runs.
func (m *Model) repoLogins() []string {
	set := map[string]bool{}
	for _, l := range m.cloneSelection() {
		set[l] = true
	}
	if entries, err := os.ReadDir(m.repoRoot()); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			l := e.Name()
			set[l] = true
			if _, ok := m.clones[l]; !ok {
				dir := filepath.Join(m.repoRoot(), l)
				ref, commit, err := repos.Inspect(dir)
				m.clones[l] = repos.Result{Target: repos.Target{Login: l, Ref: ref}, Dir: dir, Commit: commit, Err: err}
			}
		}
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// startClone clones or updates the marked repos at the tag of their graded
// submit submission.
func (m *Model) startClone() tea.Cmd {
	if m.cloning {
		return nil
	}
	var targets []repos.Target
	var skipped []string
	for _, l := range m.cloneSelection() {
		st := m.student(l)
		last := st.Last("submit")
		if last == nil || last.Ref == "" || st.Group.GitRemoteURL == "" {
			skipped = append(skipped, l)
			continue
		}
		targets = append(targets, repos.Target{Login: st.Login, Remote: st.Group.GitRemoteURL, Ref: last.Ref})
	}
	if len(targets) == 0 {
		msg := "Mark students with space in Ranking first"
		if len(skipped) > 0 {
			msg = "No submit to clone for: " + strings.Join(skipped, ", ")
		}
		return m.setStatus(msg)
	}
	m.cloning = true
	m.cloneProg = cloneProgressMsg{0, len(targets)}
	m.events = make(chan tea.Msg, 64)
	ch, root := m.events, m.repoRoot()
	go func() {
		results := repos.SyncAll(context.Background(), root, targets, 4, func(done, total int) {
			select {
			case ch <- cloneProgressMsg{done, total}:
			default:
			}
		})
		ch <- cloneDoneMsg{results}
	}()
	return m.listen()
}

// export writes the suspicious pairs of the current submission definition to CSV.
func (m *Model) export() (string, error) {
	name := fmt.Sprintf("tpi-%s-%s-suspects.csv", m.slug.Raw, m.sd)
	f, err := os.Create(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"kind", "student_a", "student_b", "level", "score", "reason",
		"overlap", "identical", "evidence", "shared", "rare_shared", "details"})
	for _, p := range m.pairsTraces() {
		_ = w.Write([]string{"traces", p.A, p.B, p.Level().String(), fmt.Sprintf("%.3f", p.Score), p.Reason(),
			fmt.Sprintf("%.3f", p.Jaccard), fmt.Sprintf("%.3f", p.Identical), fmt.Sprintf("%.3f", p.Evidence),
			fmt.Sprint(len(p.Shared)), fmt.Sprint(p.Rare),
			fmt.Sprintf("%d shared failures, %d with identical output", len(p.Shared), p.SameMsg)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	abs, _ := filepath.Abs(name)
	return abs, nil
}

// compareMarked opens a comparison for the marked students (plus cursor).
func (m *Model) compareMarked(cursor string) tea.Cmd {
	var ls []string
	for l, ok := range m.marked {
		if ok {
			ls = append(ls, l)
		}
	}
	sort.Strings(ls)
	switch {
	case len(ls) == 2:
		m.push(newCompareView(ls[0], ls[1]))
	case len(ls) == 1 && cursor != "" && cursor != ls[0]:
		m.push(newCompareView(ls[0], cursor))
	default:
		return m.setStatus("Mark 2 students with space (or 1 and put the cursor on another) and press c")
	}
	return nil
}
