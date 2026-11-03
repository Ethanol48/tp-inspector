package analysis

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Winnowing parameters (as in MOSS): k-grams of normalized tokens, keeping
// the smallest hash of every window of WinnowWindow consecutive k-grams.
const (
	KGram        = 12
	WinnowWindow = 8
	MinFuncToks  = 20    // shorter bodies are too generic to compare
	MinFuncFps   = 4     // functions left with fewer specific fingerprints are skipped
	MatchSim     = 0.5   // function pairs below this similarity are not matches
	MinSpecific  = 0.25  // shared specific fingerprints must cover this share of the smaller function
	CodeEvidence = 200.0 // matched body tokens at which a pair gets ~63% of its similarity as score
	maxFileSize  = 200 << 10
)

// Repo is the source code of one student.
type Repo struct {
	Login string
	Dir   string
	Files []string // relative paths of the analysed sources
	Funcs []Func
	// File-level stats for the AI heuristics.
	CodeLines, CommentLines int
	Comments                []string   // every comment and docstring
	Strings                 []string   // every string literal
	Lang                    Lang       // dominant language
	fps                     [][]uint64 // per function, normalized tokens
	xfps                    [][]uint64 // per function, exact tokens
}

var skipDirs = set(".git .github .idea .vscode __pycache__ .venv venv env node_modules build dist cmake-build-debug out bin obj .pytest_cache")

// LoadRepo reads the C and Python sources of a checkout.
func LoadRepo(login, dir string) (*Repo, error) {
	r := &Repo{Login: login, Dir: dir}
	count := map[Lang]int{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		lang := LangOf(p)
		if lang == LangNone || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileSize {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		src := string(b)
		r.Files = append(r.Files, rel)
		r.Funcs = append(r.Funcs, ExtractFuncs(rel, src)...)
		r.addFileStats(lang, src)
		count[lang]++
		return nil
	})
	if err != nil {
		return nil, err
	}
	if count[LangPython] > count[LangC] {
		r.Lang = LangPython
	} else if count[LangC] > 0 {
		r.Lang = LangC
	}
	for i := range r.Funcs {
		r.fps = append(r.fps, fingerprints(r.Funcs[i].Norm))
		r.xfps = append(r.xfps, fingerprints(r.Funcs[i].Exact))
	}
	return r, nil
}

func (r *Repo) addFileStats(lang Lang, src string) {
	var toks []token
	if lang == LangC {
		toks = lexC(src)
	} else {
		toks = lexPython(src)
	}
	code, comment := map[int]bool{}, map[int]bool{}
	for _, t := range toks {
		switch t.kind {
		case tComment:
			r.Comments = append(r.Comments, t.text)
			for l := t.line; l <= t.line+strings.Count(t.text, "\n"); l++ {
				comment[l] = true
			}
			continue
		case tString:
			r.Strings = append(r.Strings, t.text)
			if lang == LangPython && strings.HasPrefix(strings.TrimLeft(t.text, "rRbBuUfF"), `"""`) && t.indent >= 0 {
				// A bare triple-quoted string is a docstring.
				r.Comments = append(r.Comments, t.text)
				for l := t.line; l <= t.line+strings.Count(t.text, "\n"); l++ {
					comment[l] = true
				}
				continue
			}
		}
		code[t.line] = true
	}
	r.CodeLines += len(code)
	r.CommentLines += len(comment)
}

// fingerprints winnows the k-gram hashes of a token sequence.
func fingerprints(norm []string) []uint64 {
	if len(norm) < MinFuncToks {
		return nil
	}
	n := len(norm) - KGram + 1
	if n <= 0 {
		return nil
	}
	hs := make([]uint64, n)
	for i := range n {
		h := fnv.New64a()
		for _, t := range norm[i : i+KGram] {
			h.Write([]byte(t))
			h.Write([]byte{0})
		}
		hs[i] = h.Sum64()
	}
	seen := map[uint64]bool{}
	var out []uint64
	w := min(WinnowWindow, len(hs))
	for i := 0; i+w <= len(hs); i++ {
		best := i
		for j := i; j < i+w; j++ {
			if hs[j] <= hs[best] {
				best = j
			}
		}
		if !seen[hs[best]] {
			seen[hs[best]] = true
			out = append(out, hs[best])
		}
	}
	return out
}

// FuncMatch is a function of A that looks like a function of B.
type FuncMatch struct {
	A, B      *Func
	Sim       float64 // shared fingerprints / fingerprints of the smaller one
	Tokens    int     // normalized tokens of the smaller one
	Identical bool    // same code once whitespace and comments are ignored
}

// CodePair is a pair of students sharing code.
type CodePair struct {
	A, B    string
	Score   float64 // 1 − (1 − Sim × Evidence)(1 − Identical)
	Sim     float64 // token-weighted similarity of the matched functions
	Matched int     // normalized tokens in matched functions
	Same    int     // matched functions identical but for whitespace and comments
	// Evidence grows with the amount of matched code: one short function
	// alike proves little.
	Evidence float64
	// Identical grows with the code identical but for whitespace and
	// comments, as Evidence does.
	Identical float64
	Matches   []FuncMatch // best first
	FuncsA    int         // comparable functions of each
	FuncsB    int
	Repos     int // repos compared
}

// Level of the pair.
func (p CodePair) Level() Level { return LevelOf(p.Score) }

// Reason summarizes in words why the pair stands out.
func (p CodePair) Reason() string {
	s := plural(len(p.Matches), "function alike", "functions alike")
	if p.Same > 0 {
		s += fmt.Sprintf(", %d identical", p.Same)
	}
	if len(p.Matches) > 0 {
		m := p.Matches[0]
		s += fmt.Sprintf(" (best: %s ↔ %s, %d%%)", m.A.Name, m.B.Name, int(m.Sim*100))
	}
	return s
}

// CommonMax is the number of repos above which a fingerprint is template
// or textbook code rather than a copy: 20% of the class, at least 3 (a copy
// ring in a small sample) and at most 30 (copy rings are smaller, and every
// shared fingerprint costs a comparison per pair of repos having it).
func CommonMax(n int) int { return max(3, min(30, n/5)) }

// CompareRepos finds pairs of repos sharing code. Code found in more than
// CommonMax(len(repos)) repos (the subject's skeleton, idioms everyone uses)
// is ignored.
func CompareRepos(repos []*Repo) []CodePair {
	n := len(repos)
	if n < 2 {
		return nil
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Login < repos[j].Login })
	common := CommonMax(n)
	type fkey struct{ ra, fa, rb, fb int }
	// Two layers: normalized tokens see through renaming, but every student
	// writing the same algorithm looks alike there, so its common shapes are
	// dropped; exact tokens (what diffing two clang-formatted files shows)
	// keep a copy of a common algorithm that only a few repos share as written.
	layers := []func(*Repo) [][]uint64{
		func(r *Repo) [][]uint64 { return r.fps },
		func(r *Repo) [][]uint64 { return r.xfps },
	}
	kept := make([][][]int, len(layers)) // layer -> repo -> function -> specific fingerprints
	shared := make([]map[fkey]int, len(layers))
	for li, fpsOf := range layers {
		df := map[uint64]int{}
		for _, r := range repos {
			seen := map[uint64]bool{}
			for _, fp := range fpsOf(r) {
				for _, h := range fp {
					if !seen[h] {
						seen[h] = true
						df[h]++
					}
				}
			}
		}
		type occ struct{ repo, fn int }
		index := map[uint64][]occ{}
		kept[li] = make([][]int, n)
		for ri, r := range repos {
			kept[li][ri] = make([]int, len(fpsOf(r)))
			for fi, fp := range fpsOf(r) {
				k := 0
				for _, h := range fp {
					if df[h] <= common {
						k++
					}
				}
				if k < MinFuncFps {
					continue
				}
				kept[li][ri][fi] = k
				for _, h := range fp {
					if df[h] >= 2 && df[h] <= common {
						index[h] = append(index[h], occ{ri, fi})
					}
				}
			}
		}
		shared[li] = map[fkey]int{}
		for _, occs := range index {
			for x := range occs {
				for y := x + 1; y < len(occs); y++ {
					a, b := occs[x], occs[y]
					if a.repo == b.repo {
						continue
					}
					if a.repo > b.repo {
						a, b = b, a
					}
					shared[li][fkey{a.repo, a.fn, b.repo, b.fn}]++
				}
			}
		}
	}
	type cand struct {
		fa, fb int
		sim    float64
	}
	byPair := map[[2]int][]cand{}
	best := map[fkey]float64{}
	for li, fpsOf := range layers {
		for k, s := range shared[li] {
			den := min(kept[li][k.ra][k.fa], kept[li][k.rb][k.fb])
			if den == 0 {
				continue
			}
			sim := math.Min(1, float64(s)/float64(den))
			// A few specific fingerprints in a mostly common function match by
			// chance: they must also cover a good part of the whole function.
			whole := min(len(fpsOf(repos[k.ra])[k.fa]), len(fpsOf(repos[k.rb])[k.fb]))
			if sim < MatchSim || s < MinFuncFps || float64(s) < MinSpecific*float64(whole) {
				continue
			}
			best[k] = math.Max(best[k], sim)
		}
	}
	// Whole functions identical as written, too short to keep enough
	// specific fingerprints, still count when few repos share them.
	whole := map[string][][2]int{}
	for ri, r := range repos {
		for fi, f := range r.Funcs {
			if len(f.Exact) >= MinFuncToks {
				key := strings.Join(f.Exact, " ")
				whole[key] = append(whole[key], [2]int{ri, fi})
			}
		}
	}
	for _, occs := range whole {
		repoSet := map[int]bool{}
		for _, o := range occs {
			repoSet[o[0]] = true
		}
		if len(repoSet) < 2 || len(repoSet) > common {
			continue
		}
		for x := range occs {
			for y := x + 1; y < len(occs); y++ {
				a, b := occs[x], occs[y]
				if a[0] == b[0] {
					continue
				}
				if a[0] > b[0] {
					a, b = b, a
				}
				best[fkey{a[0], a[1], b[0], b[1]}] = 1
			}
		}
	}
	for k, sim := range best {
		key := [2]int{k.ra, k.rb}
		byPair[key] = append(byPair[key], cand{k.fa, k.fb, sim})
	}
	comparable := func(ri int) int {
		c := 0
		for fi := range kept[0][ri] {
			if kept[0][ri][fi] > 0 || kept[1][ri][fi] > 0 {
				c++
			}
		}
		return c
	}
	var out []CodePair
	for key, cs := range byPair {
		ra, rb := repos[key[0]], repos[key[1]]
		// Greedy one-to-one matching, best similarity first.
		sort.Slice(cs, func(i, j int) bool {
			if cs[i].sim != cs[j].sim {
				return cs[i].sim > cs[j].sim
			}
			return cs[i].fa < cs[j].fa || cs[i].fa == cs[j].fa && cs[i].fb < cs[j].fb
		})
		usedA, usedB := map[int]bool{}, map[int]bool{}
		p := CodePair{A: ra.Login, B: rb.Login, FuncsA: comparable(key[0]), FuncsB: comparable(key[1]), Repos: n}
		var wsum float64
		same := 0 // tokens of identical functions
		for _, c := range cs {
			if usedA[c.fa] || usedB[c.fb] {
				continue
			}
			usedA[c.fa], usedB[c.fb] = true, true
			fa, fb := &ra.Funcs[c.fa], &rb.Funcs[c.fb]
			t := min(len(fa.Norm), len(fb.Norm))
			ident := slices.Equal(fa.Exact, fb.Exact)
			p.Matches = append(p.Matches, FuncMatch{A: fa, B: fb, Sim: c.sim, Tokens: t, Identical: ident})
			p.Matched += t
			wsum += c.sim * float64(t)
			if ident {
				p.Same++
				same += t
			}
		}
		if p.Matched == 0 {
			continue
		}
		p.Sim = wsum / float64(p.Matched)
		p.Evidence = squash(float64(p.Matched), CodeEvidence)
		// Identical functions, names included, are evidence on their own:
		// they are not averaged away by the functions only alike.
		p.Identical = squash(float64(same), CodeEvidence)
		p.Score = 1 - (1-p.Sim*p.Evidence)*(1-p.Identical)
		if p.Score < 0.1 {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].A+out[i].B < out[j].A+out[j].B
	})
	return out
}