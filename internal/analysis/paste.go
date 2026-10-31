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

// PasteReport says whether a repo shows copy/paste or code written too fast.
// Hints only: students do commit in batches, and copy their own code.
type PasteReport struct {
	NoHistory   bool // git history unreadable
	Commits     int
	Lines       int // source lines added (the skeleton's first commit left out)
	Biggest     int // lines added by the biggest commit
	BigCommits  int // commits adding at least BigCommit lines
	Span        time.Duration
	Rate        float64 // lines per minute from the first to the last commit
	MaxRate     float64 // lines per minute of the fastest commit
	Bursts      []Burst
	SkippedRoot bool

	CodeLines int // lines of code scanned
	DupLines  int // lines of code repeated elsewhere in the repo
	DupRatio  float64
	Dups      []DupBlock // the longest blocks

	Pace    float64 // 0..1: bursts and overall speed
	Paste   float64 // 0..1: one commit holds a big block
	Dup     float64 // 0..1: code repeated inside the files
	Score   float64 // 1 - (1-Pace)(1-Paste)(1-Dup)
	Reasons []string
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
	hash  string
	at    time.Time
	added int // lines of source files added
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
// the source lines each one adds.
func gitPace(dir string) ([]paceCommit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "log", "--reverse", "--no-merges", "--numstat", "--format=@%at %h", "HEAD").Output()
	if err != nil {
		return nil, err
	}
	var cs []paceCommit
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		if line[0] == '@' {
			f := strings.Fields(line[1:])
			if len(f) < 2 {
				continue
			}
			sec, err := strconv.ParseInt(f[0], 10, 64)
			if err != nil {
				continue
			}
			cs = append(cs, paceCommit{hash: f[1], at: time.Unix(sec, 0)})
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
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].at.Before(cs[j].at) })
	return cs, nil
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
	files []fileLines
	hits  []dupHit
	lines int
}

type winPos struct{ file, idx int }

// scanDup finds the windows of code lines that occur twice or more in a repo
// without overlapping.
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
		var tx []string
		for i, l := range strings.Split(string(b), "\n") {
			if t, ok := dupNormalize(l, ext); ok {
				tx = append(tx, t)
				fl.orig = append(fl.orig, i+1)
			}
		}
		rd.files = append(rd.files, fl)
		texts = append(texts, tx)
		rd.lines += len(tx)
		return nil
	})

	pos := map[uint64][]winPos{}
	for fi, tx := range texts {
		for i := 0; i+DupWindow <= len(tx); i++ {
			w := tx[i : i+DupWindow]
			same := true // a run of the same line is a repetition, not a copy
			for _, x := range w[1:] {
				if x != w[0] {
					same = false
					break
				}
			}
			if same {
				continue
			}
			h := fnv.New64a()
			for _, x := range w {
				h.Write([]byte(x))
				h.Write([]byte{0})
			}
			k := h.Sum64()
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
	}

	clamp := func(x float64) float64 { return math.Max(0, math.Min(1, x)) }
	burstLines := 0
	for _, b := range r.Bursts {
		burstLines += b.Lines
	}
	pBurst := 1 - math.Exp(-float64(burstLines)/300)
	pRate := clamp((r.Rate - 3) / 7) // 3 lines/min is a human pace, 10 is a paste
	r.Pace = 1 - (1-pBurst)*(1-pRate)
	if r.Lines >= 150 && r.Biggest > 100 {
		r.Paste = (1 - math.Exp(-float64(r.Biggest-100)/300)) * float64(r.Biggest) / float64(r.Lines)
	}
	r.Dup = (1 - math.Exp(-float64(r.DupLines)/80)) * math.Min(1, r.DupRatio*8)
	r.Score = 1 - (1-r.Pace)*(1-r.Paste)*(1-r.Dup)

	type reason struct {
		w float64
		s string
	}
	var rs []reason
	if len(r.Bursts) > 0 {
		rs = append(rs, reason{pBurst, fmt.Sprintf("%d commits add %d lines within minutes of the previous one", len(r.Bursts), burstLines)})
	}
	if pRate > 0 {
		rs = append(rs, reason{pRate, fmt.Sprintf("%d lines in %s (%.0f lines/min)", paceLines, r.Span.Round(time.Minute), r.Rate)})
	}
	if r.Paste > 0.05 {
		rs = append(rs, reason{r.Paste, fmt.Sprintf("one commit adds %d of the %d lines", r.Biggest, r.Lines)})
	}
	if r.Dup > 0.05 {
		rs = append(rs, reason{r.Dup, fmt.Sprintf("%d lines repeated in the files (%.0f%% of the code)", r.DupLines, r.DupRatio*100)})
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].w > rs[j].w })
	for _, x := range rs {
		r.Reasons = append(r.Reasons, x.s)
	}
	return r
}

// PasteAll analyses the repos on disk (login -> directory): git history
// for the pace, source files for the copy/paste. Whatever most repos share
// is the subject's skeleton and does not count: a first commit of the same
// size in many repos, and repeated blocks found in more than 20 % of them.
func PasteAll(dirs map[string]string, workers int) map[string]PasteReport {
	logins := make([]string, 0, len(dirs))
	for l := range dirs {
		logins = append(logins, l)
	}
	sort.Strings(logins)

	type item struct {
		cs []paceCommit
		rd repoDup
	}
	items := make([]item, len(logins))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, workers))
	for i, l := range logins {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			cs, _ := gitPace(dirs[l])
			items[i] = item{cs, scanDup(dirs[l])}
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

	out := make(map[string]PasteReport, len(logins))
	for i, l := range logins {
		it := items[i]
		skip := skelRoot && len(it.cs) > 1 && it.cs[0].added == mode
		out[l] = buildReport(it.cs, skip, it.rd, skel)
	}
	return out
}
