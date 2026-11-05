package analysis

import (
	"context"
	"fmt"
	"hash/fnv"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Thresholds of the paste and pace analysis. They are guesses: calibrate
// them on repos whose origin you know.
const (
	BurstGap   = 10 * time.Minute // a commit this close to the previous one can be a burst
	BurstLines = 60               // lines a burst commit adds at least
	BurstRate  = 30.0             // lines per minute a burst commit reaches at least
	BigCommit  = 150              // lines that make a commit a big paste
	DupWindow  = 8                // consecutive code lines that must repeat to count as copy/paste

	StyleMinLines = 30         // code lines a file needs to count in the style comparison
	ForeignLines  = 100        // lines written by another author before it counts
	CrossMaxRepos = 12         // a block found in more repos than this is shared material (skeleton, course snippet)
	CrossMinLines = 15         // lines shared with one repo before it is listed
	ClockSkew     = time.Hour  // an author date later than the commit date by more than this is forged
	SessionGap    = 2 * time.Hour // a longer pause between two commits starts a new work session
)

// Burst is a commit adding many lines right after the previous commit:
// typed by hand, that many lines take much longer.
type Burst struct {
	Hash  string
	At    time.Time
	Lines int
	Gap   time.Duration // time since the previous commit
}

// DupBlock is a run of code lines that appears elsewhere in the same repo,
// renaming of variables ignored.
type DupBlock struct {
	File       string
	Start, End int    // original line numbers
	Lines      int    // lines of code in the block (blank lines and comments left out)
	Other      string // "file:line" of another copy
}

// Author is one identity found in the git history of a repo.
type Author struct {
	Name, Email string
	Commits     int
	Lines       int // source lines added
}

// Match is code a repo has in common with the repo of another student.
type Match struct {
	Login     string
	Lines     int     // lines of code of this repo found in the other one
	Ratio     float64 // Lines / code lines of this repo
	PeerFirst time.Time // first commit of the other repo, to tell who started first
}

// PasteReport says whether a repo shows copy/paste or code written too fast.
// Hints only: students do commit in batches, and copy their own code.
type PasteReport struct {
	NoHistory   bool // git history unreadable
	Commits     int
	Lines       int // source lines added (the skeleton's first commit left out)
	Deleted     int // source lines deleted over the same commits
	Biggest     int // lines added by the biggest commit
	BigCommits  int // commits adding at least BigCommit lines
	Span        time.Duration
	Rate        float64 // lines per minute from the first to the last commit
	MaxRate     float64 // lines per minute of the fastest commit
	Bursts      []Burst
	SkippedRoot bool

	// Shape of the work.
	FirstAt, LastAt time.Time // first and last commit written by the student
	Days            int       // distinct days with a commit
	Sessions        int       // work sessions (a pause of SessionGap or more starts a new one)
	LinesPerCommit  float64
	Churn           float64 // lines deleted / lines added: ~0 means code written once, never edited

	// Authors.
	Authors        []Author // by lines added, the five biggest
	AuthorCount    int
	ForeignLines   int     // lines written by anyone but the main author
	ForeignShare   float64 // ForeignLines / lines of all authors
	CommitterDiff  int     // commits whose committer is not their author

	// Dates.
	FutureCommits  int // commits dated in the future
	ForgedDates    int // author date later than the commit date
	RewrittenDates int // commit date more than a day after the author date (rebase, amend, --date)
	OutOfOrder     int // commits dated before their own parent
	SameSecond     int // commits sharing their exact second with at least two others (scripted history)

	// Style.
	CommentRatio float64  // comment lines / (comment + code lines)
	StyleAxes    int      // aspects of style that differ between files
	StyleNotes   []string // one line per aspect, files listed

	// Inside the files.
	CodeLines int // lines of code scanned
	DupLines  int // lines of code repeated elsewhere in the repo
	DupRatio  float64
	Dups      []DupBlock // the longest blocks

	// Between the repos.
	SharedLines int     // lines of code found in the listed repos
	SharedRatio float64 // SharedLines / CodeLines
	Matches     []Match // the repos sharing the most code, biggest first

	Pace    float64 // 0..1: bursts and overall speed
	Paste   float64 // 0..1: one commit holds a big block
	Dup     float64 // 0..1: code repeated inside the files
	Copy    float64 // 0..1: code shared with another student's repo
	Hist    float64 // 0..1: other authors, forged dates, code never edited
	Style   float64 // 0..1: files written in different styles (weak hint)
	Score   float64 // 1 - (1-Pace)(1-Paste)(1-Dup)(1-Copy)(1-Hist)(1-Style)
	Reasons []string

	why []reason
}

type reason struct {
	w float64
	s string
}

func (r *PasteReport) addReason(w float64, s string) { r.why = append(r.why, reason{w, s}) }

// finalize computes the score and sorts the reasons, strongest first.
func (r *PasteReport) finalize() {
	r.Score = 1 - (1-r.Pace)*(1-r.Paste)*(1-r.Dup)*(1-r.Copy)*(1-r.Hist)*(1-r.Style)
	rs := append([]reason(nil), r.why...)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].w > rs[j].w })
	r.Reasons = nil
	for _, x := range rs {
		r.Reasons = append(r.Reasons, x.s)
	}
}

// Level of the score.
func (r PasteReport) Level() Level { return LevelOf(r.Score) }

// Reason summarizes the strongest signals.
func (r PasteReport) Reason() string {
	if len(r.Reasons) == 0 {
		return "nothing unusual"
	}
	return strings.Join(r.Reasons[:min(3, len(r.Reasons))], ", ")
}

// ---- git history ---------------------------------------------------------

type paceCommit struct {
	hash, full, parent string
	at, committed      time.Time // author date, commit date
	author, email      string
	committer          string
	added, deleted     int  // lines of source files
	inverted           bool // dated before its own parent
}

func isSourcePath(path string) bool {
	if i := strings.Index(path, " => "); i >= 0 { // rename: "dir/{a.c => b.c}"
		path = path[i+4:]
	}
	path = strings.TrimRight(path, "}")
	switch filepath.Ext(path) {
	case ".c", ".h", ".py":
		return true
	}
	return false
}

// gitPace lists the commits of the checked-out history, oldest first, with
// the source lines each one adds and deletes.
func gitPace(dir string) ([]paceCommit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "log", "--reverse", "--no-merges", "--numstat",
		"--format=@%at%x1f%ct%x1f%H%x1f%P%x1f%h%x1f%an%x1f%ae%x1f%cn", "HEAD").Output()
	if err != nil {
		return nil, err
	}
	var cs []paceCommit
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		if line[0] == '@' {
			f := strings.Split(line[1:], "\x1f")
			if len(f) < 8 {
				continue
			}
			at, err1 := strconv.ParseInt(f[0], 10, 64)
			ct, err2 := strconv.ParseInt(f[1], 10, 64)
			if err1 != nil || err2 != nil {
				continue
			}
			c := paceCommit{at: time.Unix(at, 0), committed: time.Unix(ct, 0), full: f[2], hash: f[4],
				author: f[5], email: f[6], committer: f[7]}
			if ps := strings.Fields(f[3]); len(ps) > 0 {
				c.parent = ps[0]
			}
			cs = append(cs, c)
			continue
		}
		if len(cs) == 0 {
			continue
		}
		p := strings.SplitN(line, "\t", 3)
		if len(p) != 3 || !isSourcePath(p[2]) {
			continue
		}
		if n, err := strconv.Atoi(p[0]); err == nil { // "-" for binary files
			cs[len(cs)-1].added += n
		}
		if n, err := strconv.Atoi(p[1]); err == nil {
			cs[len(cs)-1].deleted += n
		}
	}
	// A commit dated before its own parent: the date was changed after the fact.
	when := make(map[string]time.Time, len(cs))
	for _, c := range cs {
		when[c.full] = c.at
	}
	for i := range cs {
		if pt, ok := when[cs[i].parent]; ok && cs[i].at.Before(pt.Add(-time.Minute)) {
			cs[i].inverted = true
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].at.Before(cs[j].at) })
	return cs, nil
}

func authorID(c paceCommit) string {
	id := strings.ToLower(strings.TrimSpace(c.author))
	if id == "" {
		id = strings.ToLower(strings.TrimSpace(c.email))
	}
	return id
}

// history fills the shape of the work, the authors and the date checks of
// the commits cs[start:] (the ones the student wrote).
func (r *PasteReport) history(cs []paceCommit, start int) {
	now := time.Now()
	tail := cs[start:]
	r.FirstAt, r.LastAt = tail[0].at, tail[len(tail)-1].at

	days := map[string]bool{}
	r.Sessions = 1
	same := map[int64]int{}
	for i, c := range tail {
		days[c.at.Local().Format("2006-01-02")] = true
		if i > 0 && c.at.Sub(tail[i-1].at) >= SessionGap {
			r.Sessions++
		}
		if c.at.After(now.Add(10*time.Minute)) || c.committed.After(now.Add(10*time.Minute)) {
			r.FutureCommits++
		}
		if c.at.After(c.committed.Add(ClockSkew)) {
			r.ForgedDates++
		}
		if c.committed.Sub(c.at) > 24*time.Hour {
			r.RewrittenDates++
		}
		if c.inverted {
			r.OutOfOrder++
		}
		same[c.at.Unix()]++
	}
	r.Days = len(days)
	for _, n := range same {
		if n >= 3 {
			r.SameSecond += n
		}
	}
	r.LinesPerCommit = float64(r.Lines) / float64(len(tail))
	r.authors(cs, start)
}

// authors tells who wrote the lines. A first commit by someone else than the
// main author is the starter code, not the student's.
func (r *PasteReport) authors(cs []paceCommit, start int) {
	tally := func(from int) map[string]*Author {
		m := map[string]*Author{}
		for _, c := range cs[from:] {
			id := authorID(c)
			a := m[id]
			if a == nil {
				a = &Author{Name: c.author, Email: c.email}
				m[id] = a
			}
			a.Commits++
			a.Lines += c.added
		}
		return m
	}
	top := func(m map[string]*Author) string {
		best := ""
		for id, a := range m {
			if best == "" {
				best = id
				continue
			}
			b := m[best]
			if a.Lines > b.Lines || a.Lines == b.Lines && (a.Commits > b.Commits || a.Commits == b.Commits && id < best) {
				best = id
			}
		}
		return best
	}
	from := start
	if from == 0 && len(cs) > 1 && authorID(cs[0]) != top(tally(1)) {
		from = 1
	}
	m := tally(from)
	main := top(m)
	list := make([]Author, 0, len(m))
	total := 0
	for _, a := range m {
		list = append(list, *a)
		total += a.Lines
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Lines != list[j].Lines {
			return list[i].Lines > list[j].Lines
		}
		if list[i].Commits != list[j].Commits {
			return list[i].Commits > list[j].Commits
		}
		return list[i].Name < list[j].Name
	})
	r.AuthorCount = len(list)
	if main != "" {
		r.ForeignLines = total - m[main].Lines
	}
	if total > 0 {
		r.ForeignShare = float64(r.ForeignLines) / float64(total)
	}
	if len(list) > 5 {
		list = list[:5]
	}
	r.Authors = list
	for _, c := range cs[from:] {
		if !strings.EqualFold(strings.TrimSpace(c.author), strings.TrimSpace(c.committer)) {
			r.CommitterDiff++
		}
	}
}

// ---- copy/paste inside the files ----------------------------------------

var (
	dupStrRe   = regexp.MustCompile(`"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'`)
	dupNumRe   = regexp.MustCompile(`\b(?:0[xX][0-9a-fA-F]+|\d+(?:\.\d+)?)\b`)
	dupIdentRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

	dupSkipDirs = map[string]bool{"build": true, "_build": true, "node_modules": true, "venv": true, "__pycache__": true, "dist": true, "out": true, "target": true}

	dupKeywords = wordSet("if else for while do switch case default break continue return goto sizeof struct union enum typedef static const void int char long short unsigned signed float double size_t NULL true false " +
		"def class import from as in not and or is None True False elif try except finally raise with lambda pass yield global del assert self")
)

// dupNormalize reduces a line of code to its shape: identifiers become I,
// numbers and strings are masked, comments and spaces dropped. Lines with
// nothing left to compare ("}", comments, blank) are skipped.
func dupNormalize(line, ext string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" {
		return "", false
	}
	if ext == ".py" {
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, `"""`) || strings.HasPrefix(t, "'''") {
			return "", false
		}
	} else if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "#") {
		return "", false
	}
	t = dupStrRe.ReplaceAllString(t, "\x01")
	if ext == ".py" {
		if i := strings.Index(t, "#"); i >= 0 {
			t = t[:i]
		}
	} else if i := strings.Index(t, "//"); i >= 0 {
		t = t[:i]
	}
	t = dupNumRe.ReplaceAllString(t, "\x02")
	t = dupIdentRe.ReplaceAllStringFunc(t, func(w string) string {
		if dupKeywords[w] {
			return w
		}
		return "I"
	})
	t = strings.Join(strings.Fields(t), "")
	if len(t) < 4 {
		return "", false
	}
	return t, true
}

// stripComment cuts a trailing comment, quotes respected.
func stripComment(t, ext string) string {
	var q byte
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case q != 0:
			if c == '\\' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case ext == ".py" && c == '#':
			return t[:i]
		case ext != ".py" && c == '/' && i+1 < len(t) && t[i+1] == '/':
			return t[:i]
		}
	}
	return t
}

// lightNormalize keeps the names, numbers and strings of a line (only the
// spaces and the comment go): two students sharing eight such lines in a row
// have the same code, not just the same shape.
func lightNormalize(t, ext string) string {
	return strings.Join(strings.Fields(stripComment(t, ext)), "")
}

// commentLine tells whether the (trimmed, non-empty) line is a comment;
// inBlock and delim carry the state of /* */ and """ """ across lines.
func commentLine(t, ext string, inBlock *bool, delim *string) bool {
	if ext == ".py" {
		if *inBlock {
			if strings.Contains(t, *delim) {
				*inBlock = false
			}
			return true
		}
		for _, d := range []string{`"""`, `'''`} {
			if strings.HasPrefix(t, d) {
				if !strings.Contains(t[3:], d) {
					*inBlock, *delim = true, d
				}
				return true
			}
		}
		return strings.HasPrefix(t, "#")
	}
	if *inBlock {
		if strings.Contains(t, "*/") {
			*inBlock = false
		}
		return true
	}
	if strings.HasPrefix(t, "/*") {
		if !strings.Contains(t[2:], "*/") {
			*inBlock = true
		}
		return true
	}
	return strings.HasPrefix(t, "//")
}

// ---- style of the files ---------------------------------------------------

// fileStyle is how one file is written; "" means not enough to tell.
type fileStyle struct {
	name     string
	code     int    // lines of code
	indent   string // tabs, 2 spaces, 4 spaces, mixed
	brace    string // same-line, own-line, mixed
	naming   string // snake, camel, mixed
	comments string // dense, sparse, normal
}

type styleCount struct {
	tabs, spaces, mix, sp2, sp4 int
	own, same                   int
	snake, camel                int
}

// nameKind: 1 for snake_case, 2 for lowerCamelCase, 0 for the rest
// (macros, types, short or reserved names).
func nameKind(w string) int {
	if len(w) < 5 || w[0] == '_' || dupKeywords[w] || strings.HasSuffix(w, "_t") {
		return 0
	}
	lower, upper := false, false
	for i := 0; i < len(w); i++ {
		switch c := w[i]; {
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= 'A' && c <= 'Z':
			upper = true
		}
	}
	if !lower {
		return 0
	}
	if strings.Contains(w, "_") {
		if !upper {
			return 1
		}
		return 0
	}
	if upper && w[0] >= 'a' && w[0] <= 'z' {
		return 2
	}
	return 0
}

func (s *styleCount) add(raw, t, ext string) {
	lead := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]
	if lead != "" {
		hasTab, hasSpace := strings.Contains(lead, "\t"), strings.Contains(lead, " ")
		switch {
		case hasTab && hasSpace:
			s.mix++
		case hasTab:
			s.tabs++
		default:
			s.spaces++
			if len(lead)%4 == 0 {
				s.sp4++
			} else if len(lead)%2 == 0 {
				s.sp2++
			}
		}
	}
	if ext != ".py" {
		if t == "{" {
			s.own++
		} else if strings.HasSuffix(t, "{") && !strings.Contains(t, "=") {
			s.same++
		}
	}
	code := dupStrRe.ReplaceAllString(stripComment(t, ext), " ")
	for _, w := range dupIdentRe.FindAllString(code, -1) {
		switch nameKind(w) {
		case 1:
			s.snake++
		case 2:
			s.camel++
		}
	}
}

func (s styleCount) label(name string, code, com int) fileStyle {
	f := fileStyle{name: name, code: code}
	if tot := s.tabs + s.spaces + s.mix; tot >= 10 {
		switch {
		case float64(s.tabs) >= 0.8*float64(tot):
			f.indent = "tabs"
		case float64(s.spaces) >= 0.8*float64(tot):
			if float64(s.sp2) >= 0.2*float64(s.spaces) {
				f.indent = "2 spaces"
			} else {
				f.indent = "4 spaces"
			}
		default:
			f.indent = "mixed"
		}
	}
	if bt := s.own + s.same; bt >= 6 {
		switch {
		case float64(s.own) >= 0.8*float64(bt):
			f.brace = "own-line"
		case float64(s.same) >= 0.8*float64(bt):
			f.brace = "same-line"
		default:
			f.brace = "mixed"
		}
	}
	if nt := s.snake + s.camel; nt >= 8 {
		switch {
		case s.camel*4 <= s.snake:
			f.naming = "snake"
		case s.snake*4 <= s.camel:
			f.naming = "camel"
		default:
			f.naming = "mixed"
		}
	}
	if code >= StyleMinLines {
		ratio := float64(com) / float64(com+code)
		switch {
		case ratio >= 0.25:
			f.comments = "dense"
		case ratio <= 0.04:
			f.comments = "sparse"
		default:
			f.comments = "normal"
		}
	}
	return f
}

// styleSignal lists the aspects of style that differ between the files of a
// repo (indentation, braces, naming, comments), with the files concerned.
func styleSignal(styles []fileStyle) (names, notes []string) {
	axes := []struct {
		name string
		get  func(fileStyle) string
	}{
		{"indentation", func(f fileStyle) string { return f.indent }},
		{"braces", func(f fileStyle) string { return f.brace }},
		{"naming", func(f fileStyle) string { return f.naming }},
		{"comments", func(f fileStyle) string {
			if f.comments == "normal" {
				return ""
			}
			return f.comments
		}},
	}
	for _, ax := range axes {
		groups := map[string][]string{}
		for _, f := range styles {
			if f.code < StyleMinLines {
				continue
			}
			if l := ax.get(f); l != "" {
				groups[l] = append(groups[l], f.name)
			}
		}
		if len(groups) < 2 {
			continue
		}
		labels := make([]string, 0, len(groups))
		for l := range groups {
			labels = append(labels, l)
		}
		sort.Slice(labels, func(i, j int) bool {
			a, b := groups[labels[i]], groups[labels[j]]
			if len(a) != len(b) {
				return len(a) > len(b)
			}
			return labels[i] < labels[j]
		})
		parts := make([]string, 0, len(labels))
		for _, l := range labels {
			files, more := groups[l], ""
			if len(files) > 3 {
				more = fmt.Sprintf(" +%d", len(files)-3)
				files = files[:3]
			}
			parts = append(parts, l+" in "+strings.Join(files, ", ")+more)
		}
		names = append(names, ax.name)
		notes = append(notes, ax.name+": "+strings.Join(parts, " · "))
	}
	return names, notes
}

// ---- windows of code ------------------------------------------------------

func sameRun(w []string) bool { // a run of the same line is a repetition, not a copy
	for _, x := range w[1:] {
		if x != w[0] {
			return false
		}
	}
	return true
}

func windowHash(w []string) uint64 {
	h := fnv.New64a()
	for _, x := range w {
		h.Write([]byte(x))
		h.Write([]byte{0})
	}
	if k := h.Sum64(); k != 0 {
		return k
	}
	return 1
}

// windows hashes every run of DupWindow lines; 0 where there is none.
func windows(lines []string) []uint64 {
	ws := make([]uint64, len(lines))
	for i := 0; i+DupWindow <= len(lines); i++ {
		if w := lines[i : i+DupWindow]; !sameRun(w) {
			ws[i] = windowHash(w)
		}
	}
	return ws
}

type fileLines struct {
	name string
	orig []int // original line number of each code line
}

type dupHit struct {
	file, idx int // window of DupWindow code lines starting at idx
	hash      uint64
	other     string
}

type repoDup struct {
	files    []fileLines
	hits     []dupHit
	win      [][]uint64  // per file: hash of the window starting at each code line, names kept
	styles   []fileStyle // per file
	lines    int         // lines of code
	comments int         // lines of comments
}

type winPos struct{ file, idx int }

// scanDup finds the windows of code lines that occur twice or more in a repo
// without overlapping, and records what the other checks need: the windows
// with names kept (to compare repos) and the style of each file.
func scanDup(dir string) repoDup {
	var rd repoDup
	var texts [][]string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != dir && (strings.HasPrefix(name, ".") || dupSkipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(name)
		if ext != ".c" && ext != ".h" && ext != ".py" {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > 200<<10 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		fl := fileLines{name: rel}
		var tx, lx []string
		var sc styleCount
		com, inBlock, delim := 0, false, ""
		for i, l := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(l)
			if t != "" && commentLine(t, ext, &inBlock, &delim) {
				com++
			}
			if s, ok := dupNormalize(l, ext); ok {
				tx = append(tx, s)
				lx = append(lx, lightNormalize(t, ext))
				fl.orig = append(fl.orig, i+1)
				sc.add(l, t, ext)
			}
		}
		rd.files = append(rd.files, fl)
		texts = append(texts, tx)
		rd.win = append(rd.win, windows(lx))
		rd.styles = append(rd.styles, sc.label(rel, len(tx), com))
		rd.lines += len(tx)
		rd.comments += com
		return nil
	})

	pos := map[uint64][]winPos{}
	for fi, tx := range texts {
		for i := 0; i+DupWindow <= len(tx); i++ {
			w := tx[i : i+DupWindow]
			if sameRun(w) {
				continue
			}
			k := windowHash(w)
			if len(pos[k]) < 40 {
				pos[k] = append(pos[k], winPos{fi, i})
			}
		}
	}
	for k, ps := range pos {
		if len(ps) < 2 {
			continue
		}
		for _, p := range ps {
			for _, q := range ps {
				d := q.idx - p.idx
				if d < 0 {
					d = -d
				}
				if q.file == p.file && d < DupWindow { // the same region
					continue
				}
				rd.hits = append(rd.hits, dupHit{file: p.file, idx: p.idx, hash: k,
					other: fmt.Sprintf("%s:%d", rd.files[q.file].name, rd.files[q.file].orig[q.idx])})
				break
			}
		}
	}
	return rd
}

// buildDup merges the windows into blocks. skel tells the windows that most
// repos share (the subject's skeleton), which are ignored.
func buildDup(rd repoDup, skel func(uint64) bool) (int, []DupBlock) {
	covered := make([][]bool, len(rd.files))
	for i, f := range rd.files {
		covered[i] = make([]bool, len(f.orig))
	}
	other := map[[2]int]string{}
	for _, h := range rd.hits {
		if skel != nil && skel(h.hash) {
			continue
		}
		for j := h.idx; j < h.idx+DupWindow; j++ {
			covered[h.file][j] = true
		}
		other[[2]int{h.file, h.idx}] = h.other
	}
	total := 0
	var blocks []DupBlock
	for fi, cv := range covered {
		for i := 0; i < len(cv); {
			if !cv[i] {
				i++
				continue
			}
			j := i
			for j < len(cv) && cv[j] {
				j++
			}
			total += j - i
			blocks = append(blocks, DupBlock{File: rd.files[fi].name, Start: rd.files[fi].orig[i], End: rd.files[fi].orig[j-1],
				Lines: j - i, Other: other[[2]int{fi, i}]})
			i = j
		}
	}
	sort.SliceStable(blocks, func(a, b int) bool { return blocks[a].Lines > blocks[b].Lines })
	return total, blocks
}

// ---- code shared between students ------------------------------------------

type pasteItem struct {
	cs []paceCommit
	rd repoDup
}

type crossInfo struct {
	matches []Match
	shared  int
}

func countTrue(c [][]bool) int {
	n := 0
	for _, row := range c {
		for _, v := range row {
			if v {
				n++
			}
		}
	}
	return n
}

// crossScan compares the repos with each other. A window of lines (names and
// numbers kept) found in a few repos is code passed from one student to
// another; found in many, it is the subject's skeleton or a snippet of the
// course and does not count. With fewer than three repos nothing can be told
// from the skeleton, so nothing is reported.
func crossScan(logins []string, items []pasteItem) []crossInfo {
	n := len(items)
	out := make([]crossInfo, n)
	if n < 2 {
		return out
	}
	limit := n - 1
	if n >= 5 {
		limit = min(CrossMaxRepos, max(3, n/5))
	}

	inv := map[uint64][]int32{}
	for i, it := range items {
		seen := map[uint64]bool{}
		for _, ws := range it.rd.win {
			for _, h := range ws {
				if h != 0 && !seen[h] {
					seen[h] = true
					inv[h] = append(inv[h], int32(i))
				}
			}
		}
	}
	pairs := make([]map[int32]int, n)
	for _, l := range inv {
		if len(l) < 2 || len(l) > limit {
			continue
		}
		for _, a := range l {
			if pairs[a] == nil {
				pairs[a] = map[int32]int{}
			}
			for _, b := range l {
				if a != b {
					pairs[a][b]++
				}
			}
		}
	}

	type partner struct {
		b int32
		w int
	}
	for a := 0; a < n; a++ {
		var ps []partner
		for b, w := range pairs[a] {
			if w >= 3 {
				ps = append(ps, partner{b, w})
			}
		}
		if len(ps) == 0 {
			continue
		}
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].w != ps[j].w {
				return ps[i].w > ps[j].w
			}
			return ps[i].b < ps[j].b
		})
		if len(ps) > 5 {
			ps = ps[:5]
		}
		rd := items[a].rd
		mk := func() [][]bool {
			c := make([][]bool, len(rd.files))
			for fi := range c {
				c[fi] = make([]bool, len(rd.files[fi].orig))
			}
			return c
		}
		cov := map[int32][][]bool{}
		for _, p := range ps {
			cov[p.b] = mk()
		}
		for fi, ws := range rd.win {
			for idx, h := range ws {
				if h == 0 {
					continue
				}
				l := inv[h]
				if len(l) < 2 || len(l) > limit {
					continue
				}
				for _, b := range l {
					if c, ok := cov[b]; ok {
						for j := idx; j < idx+DupWindow; j++ {
							c[fi][j] = true
						}
					}
				}
			}
		}
		uni := mk()
		for _, p := range ps {
			c := cov[p.b]
			lines := countTrue(c)
			if lines < CrossMinLines {
				continue
			}
			for fi := range c {
				for j, v := range c[fi] {
					if v {
						uni[fi][j] = true
					}
				}
			}
			out[a].matches = append(out[a].matches, Match{Login: logins[p.b], Lines: lines,
				Ratio: float64(lines) / float64(max(1, rd.lines))})
		}
		sort.SliceStable(out[a].matches, func(i, j int) bool { return out[a].matches[i].Lines > out[a].matches[j].Lines })
		out[a].shared = countTrue(uni)
	}
	return out
}

// applyCross adds the code shared with other students to the report.
func (r *PasteReport) applyCross(c crossInfo) {
	r.Matches = c.matches
	r.SharedLines = c.shared
	if r.CodeLines > 0 {
		r.SharedRatio = float64(c.shared) / float64(r.CodeLines)
	}
	r.Copy = (1 - math.Exp(-float64(r.SharedLines)/60)) * math.Min(1, r.SharedRatio*5)
	if r.Copy > 0.05 && len(r.Matches) > 0 {
		top := r.Matches[0]
		s := fmt.Sprintf("%d lines (%.0f%% of the code) also in the repo of %s", top.Lines, top.Ratio*100, top.Login)
		if len(r.Matches) > 1 {
			s += fmt.Sprintf(" and %d other", len(r.Matches)-1)
			if len(r.Matches) > 2 {
				s += "s"
			}
		}
		r.addReason(r.Copy, s)
	}
	r.finalize()
}

// ---- report --------------------------------------------------------------

func buildReport(cs []paceCommit, skipRoot bool, rd repoDup, skel func(uint64) bool) PasteReport {
	var r PasteReport
	r.CodeLines = rd.lines
	r.DupLines, r.Dups = buildDup(rd, skel)
	if rd.lines > 0 {
		r.DupRatio = float64(r.DupLines) / float64(rd.lines)
	}
	if len(r.Dups) > 10 {
		r.Dups = r.Dups[:10]
	}
	if rd.lines+rd.comments > 0 {
		r.CommentRatio = float64(rd.comments) / float64(rd.lines+rd.comments)
	}
	var styleNames []string
	styleNames, r.StyleNotes = styleSignal(rd.styles)
	r.StyleAxes = len(styleNames)

	r.Commits = len(cs)
	paceLines := 0
	if len(cs) == 0 {
		r.NoHistory = true
	} else {
		start := 0
		if skipRoot && len(cs) > 1 {
			start = 1
			r.SkippedRoot = true
		}
		for i := start; i < len(cs); i++ {
			r.Lines += cs[i].added
			r.Deleted += cs[i].deleted
			r.Biggest = max(r.Biggest, cs[i].added)
			if cs[i].added >= BigCommit {
				r.BigCommits++
			}
		}
		// The lines of the first commit were written before it: only the
		// following ones say how fast the code came.
		if len(cs)-start > 1 {
			r.Span = cs[len(cs)-1].at.Sub(cs[start].at)
			paceLines = r.Lines - cs[start].added
			if paceLines >= 300 {
				r.Rate = float64(paceLines) / math.Max(r.Span.Minutes(), 1)
			}
		}
		for i := max(start, 1); i < len(cs); i++ {
			gap := cs[i].at.Sub(cs[i-1].at)
			if gap > BurstGap || cs[i].added < 20 {
				continue
			}
			rate := float64(cs[i].added) / math.Max(gap.Minutes(), 1)
			r.MaxRate = math.Max(r.MaxRate, rate)
			if cs[i].added >= BurstLines && rate >= BurstRate {
				r.Bursts = append(r.Bursts, Burst{Hash: cs[i].hash, At: cs[i].at, Lines: cs[i].added, Gap: gap})
			}
		}
		r.history(cs, start)
	}

	burstLines := 0
	for _, b := range r.Bursts {
		burstLines += b.Lines
	}
	pBurst := 1 - math.Exp(-float64(burstLines)/300)
	pRate := clamp01((r.Rate - 3) / 7) // 3 lines/min is a human pace, 10 is a paste
	r.Pace = 1 - (1-pBurst)*(1-pRate)
	if r.Lines >= 150 && r.Biggest > 100 {
		r.Paste = (1 - math.Exp(-float64(r.Biggest-100)/300)) * float64(r.Biggest) / float64(r.Lines)
	}
	r.Dup = (1 - math.Exp(-float64(r.DupLines)/80)) * math.Min(1, r.DupRatio*8)

	// History: who wrote the code, were the dates touched, was it ever edited.
	pAuthors := 0.0
	if r.ForeignLines >= ForeignLines {
		pAuthors = math.Min(0.8, clamp01(r.ForeignShare*1.3)*(1-math.Exp(-float64(r.ForeignLines)/200)))
	}
	if r.Lines > 0 {
		r.Churn = float64(r.Deleted) / float64(r.Lines)
	}
	pChurn := 0.0
	if r.Lines >= 300 { // code written once and never touched again
		pChurn = clamp01((0.04-r.Churn)/0.04) * (1 - math.Exp(-float64(r.Lines)/1000)) * 0.35
	}
	pDates := clamp01(0.35*float64(r.FutureCommits+r.ForgedDates) + 0.08*float64(r.OutOfOrder))
	if r.SameSecond >= 3 {
		pDates = math.Max(pDates, 0.3)
	}
	pDates = math.Min(pDates, 0.7)
	r.Hist = 1 - (1-pAuthors)*(1-pChurn)*(1-pDates)
	r.Style = []float64{0, 0.1, 0.2, 0.3, 0.3}[min(r.StyleAxes, 4)]

	if len(r.Bursts) > 0 {
		r.addReason(pBurst, fmt.Sprintf("%d commits add %d lines within minutes of the previous one", len(r.Bursts), burstLines))
	}
	if pRate > 0 {
		r.addReason(pRate, fmt.Sprintf("%d lines in %s (%.0f lines/min)", paceLines, r.Span.Round(time.Minute), r.Rate))
	}
	if r.Paste > 0.05 {
		r.addReason(r.Paste, fmt.Sprintf("one commit adds %d of the %d lines", r.Biggest, r.Lines))
	}
	if r.Dup > 0.05 {
		r.addReason(r.Dup, fmt.Sprintf("%d lines repeated in the files (%.0f%% of the code)", r.DupLines, r.DupRatio*100))
	}
	if pAuthors > 0.05 && len(r.Authors) > 1 {
		r.addReason(pAuthors, fmt.Sprintf("%.0f%% of the lines written by another author (%s)", r.ForeignShare*100, r.Authors[1].Name))
	}
	if pChurn > 0.05 {
		r.addReason(pChurn, fmt.Sprintf("code never edited: %d lines deleted for %d added", r.Deleted, r.Lines))
	}
	if n := r.FutureCommits + r.ForgedDates; n > 0 {
		r.addReason(pDates, fmt.Sprintf("%d commits with forged or future dates", n))
	}
	if r.OutOfOrder > 0 {
		r.addReason(0.08*float64(r.OutOfOrder), fmt.Sprintf("%d commits dated before their parent", r.OutOfOrder))
	}
	if r.SameSecond >= 3 {
		r.addReason(0.3, fmt.Sprintf("%d commits share the same second (scripted history)", r.SameSecond))
	}
	if r.RewrittenDates >= 3 {
		r.addReason(0.06, fmt.Sprintf("%d commits rewritten a day or more after being written", r.RewrittenDates))
	}
	if r.StyleAxes > 0 {
		r.addReason(r.Style, "files written in different styles ("+strings.Join(styleNames, ", ")+")")
	}
	r.finalize()
	return r
}

// PasteAll analyses the repos on disk (login -> directory): git history
// for the pace, source files for the copy/paste, and the repos against each
// other for code passed between students. Whatever most repos share is the
// subject's skeleton and does not count: a first commit of the same size in
// many repos, and repeated blocks found in more than 20 % of them.
func PasteAll(dirs map[string]string, workers int) map[string]PasteReport {
	logins := make([]string, 0, len(dirs))
	for l := range dirs {
		logins = append(logins, l)
	}
	sort.Strings(logins)

	items := make([]pasteItem, len(logins))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, workers))
	for i, l := range logins {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			cs, _ := gitPace(dirs[l])
			items[i] = pasteItem{cs, scanDup(dirs[l])}
		}()
	}
	wg.Wait()

	// The skeleton's commit: the same number of lines in many repos.
	rootCount := map[int]int{}
	for _, it := range items {
		if len(it.cs) > 1 {
			rootCount[it.cs[0].added]++
		}
	}
	mode, best := 0, 0
	for v, c := range rootCount {
		if c > best || c == best && v > mode {
			mode, best = v, c
		}
	}
	skelRoot := mode > 0 && best >= max(3, len(items)*3/10)

	// The skeleton's repeated blocks: found in too many repos.
	df := map[uint64]int{}
	for _, it := range items {
		seen := map[uint64]bool{}
		for _, h := range it.rd.hits {
			if !seen[h.hash] {
				seen[h.hash] = true
				df[h.hash]++
			}
		}
	}
	thr := len(items) + 1 // never, below 5 repos
	if len(items) >= 5 {
		thr = max(3, len(items)/5)
	}
	skel := func(h uint64) bool { return df[h] > thr }

	cross := crossScan(logins, items)

	out := make(map[string]PasteReport, len(logins))
	for i, l := range logins {
		it := items[i]
		skip := skelRoot && len(it.cs) > 1 && it.cs[0].added == mode
		rep := buildReport(it.cs, skip, it.rd, skel)
		rep.applyCross(cross[i])
		out[l] = rep
	}
	// Who started first, to read the matches.
	for _, rep := range out {
		for k := range rep.Matches {
			rep.Matches[k].PeerFirst = out[rep.Matches[k].Login].FirstAt
		}
	}
	return out
}