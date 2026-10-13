package analysis

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"tp-inspector/internal/repos"
)

// MinBaseline is the number of repos from which a student's style is
// compared with the class; below it only absolute tells count.
const MinBaseline = 5

// Signal is one reason a repo looks AI-generated.
type Signal struct {
	Name   string
	Detail string  // in words, with the class reference when relevant
	Z      float64 // standard deviations above the class (0 for absolute tells)
}

// FuncFlag is a function worth reading first.
type FuncFlag struct {
	Func  *Func
	Score float64
	Why   []string
}

// AIScore says how much a repo looks AI-generated. Hints only: some students
// do write documented, defensive code.
type AIScore struct {
	Score    float64 // 1 - (1-Style)(1-Tells), at least AgentScore when a tool is credited
	Style    float64 // how far the style is from the class (needs MinBaseline repos)
	Tells    float64 // absolute tells: typography, doc tags, history
	Agent    string  // AI tool credited in the commits (Co-Authored-By: Claude…), "" if none
	Signals  []Signal
	TopFuncs []FuncFlag
	Commits  int
}

// Level of the score.
func (s AIScore) Level() Level { return LevelOf(s.Score) }

// Reason summarizes the strongest signals.
func (s AIScore) Reason() string {
	var names []string
	for i, sg := range s.Signals {
		if i == 3 {
			break
		}
		names = append(names, sg.Name)
	}
	if len(names) == 0 {
		return "nothing unusual"
	}
	return strings.Join(names, ", ")
}

var (
	docTagRe   = regexp.MustCompile(`(?m)(@brief|@param|@return|@retval|\\brief|\\param|\\return|^\s*(Args|Arguments|Returns|Raises|Parameters|Yields|Example|Examples)\s*:|^\s*:param |^\s*:return|^\s*-{3,}\s*$)`)
	commitAIRe = regexp.MustCompile(`(?i)(^(feat|fix|refactor|chore|docs|style|test|perf)(\(.+\))?:|comprehensive|robust|enhance|streamline|ensur|leverag|utiliz|edge.?cases?|for (better|improved) |readability|maintainability|implementation of)`)
	commitVerb = regexp.MustCompile(`^(Add|Implement|Refactor|Update|Improve|Enhance|Create|Introduce|Optimize|Handle|Ensure)\b`)
)

// AgentScore is the minimum AI score of a repo whose commits credit an AI
// tool: the tool says so itself.
const AgentScore = 0.95

// agentTools maps a pattern found in commit metadata to the tool's name.
var agentTools = []struct {
	name string
	re   *regexp.Regexp
}{
	// Not just "Claude": it is a common first name.
	{"Claude", regexp.MustCompile(`(?i)anthropic\.com|claude\[bot\]|claude[ -]?code|claude\.(ai|com)\b|\bclaude[ -](opus|sonnet|haiku|fable|instant|[0-9])`)},
	{"Copilot", regexp.MustCompile(`(?i)\bcopilot\b`)},
	{"Cursor", regexp.MustCompile(`(?i)cursoragent|@cursor\.(com|sh)|\bcursor agent\b`)},
	{"ChatGPT / Codex", regexp.MustCompile(`(?i)\bchatgpt\b|\bcodex\b|openai\.com|\bgpt-[345]`)},
	{"Gemini", regexp.MustCompile(`(?i)\bgemini\b|jules\.google`)},
	{"Aider", regexp.MustCompile(`(?i)\baider\b`)},
	{"Devin", regexp.MustCompile(`(?i)\bdevin(-ai)?\b.*(bot|ai)|devin-ai-integration`)},
	{"Windsurf / Codeium", regexp.MustCompile(`(?i)\bwindsurf\b|\bcodeium\b`)},
	{"Amazon Q", regexp.MustCompile(`(?i)amazon q developer|\bq developer\b`)},
}

// agentLine finds the commit metadata lines that credit a tool: co-author
// trailers, "Generated with" footers, the author or committer.
var agentLine = regexp.MustCompile(`(?im)^\s*(co-authored-by|signed-off-by|assisted-by|generated-by)\s*:.*$|^.*generated (with|by|using) .*$|^.*🤖.*$`)

// agentOf returns the AI tool a commit credits, or "".
func agentOf(c repos.Commit) string {
	cands := append(agentLine.FindAllString(c.Body+"\n"+c.Subject, -1), c.Author, c.Committer)
	for _, l := range cands {
		for _, t := range agentTools {
			if t.re.MatchString(l) {
				return t.name
			}
		}
	}
	return ""
}

// typographic reports characters that keyboards seldom produce but
// generators often do.
func typographic(r rune) bool {
	switch r {
	case '—', '–', '’', '‘', '“', '”', '→', '←', '⇒', '…', '•', '✓', '✔', '✅', '❌', '⚠', '≤', '≥', '≠', '×':
		return true
	}
	return r >= 0x1F300 && r <= 0x1FAFF // emojis
}

func countTypo(ss []string) int {
	n := 0
	for _, s := range ss {
		for _, r := range s {
			if typographic(r) {
				n++
			}
		}
	}
	return n
}

// commentLines splits comments into their text lines, without markers.
func commentLines(cs []string) []string {
	var out []string
	for _, c := range cs {
		for l := range strings.SplitSeq(c, "\n") {
			l = strings.TrimSpace(l)
			l = strings.TrimLeft(l, "/*#\"' ")
			l = strings.TrimRight(l, "*/\"' ")
			if l != "" {
				out = append(out, l)
			}
		}
	}
	return out
}

// sentence reports a comment line written as a full English-like sentence.
func sentence(l string) bool {
	r := []rune(l)
	return len(strings.Fields(l)) >= 6 && unicode.IsUpper(r[0]) && strings.ContainsRune(".:", r[len(r)-1])
}

// guard reports a function starting with an argument check.
func guard(f *Func) bool {
	b := f.Body
	if len(b) == 0 {
		return false
	}
	head := b[:min(len(b), 20)]
	has := func(ws ...string) bool {
		for _, t := range head {
			if slices.Contains(ws, t.text) {
				return true
			}
		}
		return false
	}
	if f.Lang == LangC {
		return b[0].text == "if" && has("NULL", "!", "<", "<=") && has("return", "exit", "abort")
	}
	switch b[0].text {
	case "assert", "raise":
		return true
	case "if":
		return has("not", "None", "isinstance", "<", "<=") && has("raise", "return")
	}
	return false
}

// feature is one style measure compared across the class.
type feature struct {
	name   string
	detail func(v, med float64) string
	values map[string]float64
}

// AIScores scores every repo. history maps logins to their git log; missing
// logs only disable the history signals.
func AIScores(rs []*Repo, history map[string][]repos.Commit) map[string]AIScore {
	n := len(rs)
	pct := func(v float64) string { return fmt.Sprintf("%d%%", int(math.Round(v*100))) }
	mk := func(name string, detail func(v, med float64) string) *feature {
		return &feature{name: name, detail: detail, values: map[string]float64{}}
	}
	fComments := mk("many comments", func(v, med float64) string {
		return fmt.Sprintf("%s of lines are comments (promo median %s)", pct(v), pct(med))
	})
	fSentences := mk("sentence comments", func(v, med float64) string {
		return fmt.Sprintf("%s of comment lines are full sentences (promo median %s)", pct(v), pct(med))
	})
	fDocs := mk("doc comments", func(v, med float64) string {
		return fmt.Sprintf("%s of functions have a structured doc comment (promo median %s)", pct(v), pct(med))
	})
	fGuards := mk("guard clauses", func(v, med float64) string {
		return fmt.Sprintf("%s of functions start by checking their arguments (promo median %s)", pct(v), pct(med))
	})
	fIdent := mk("long names", func(v, med float64) string {
		return fmt.Sprintf("identifiers average %.1f characters (promo median %.1f)", v, med)
	})
	fRare := mk("unusual constructs", func(v, med float64) string {
		return fmt.Sprintf("%.0f keywords / library calls almost nobody else uses (promo median %.0f)", v, med)
	})
	fFew := mk("few commits", func(v, med float64) string {
		return fmt.Sprintf("%.0f commits (promo median %.0f)", -v, -med)
	})
	fBig := mk("code in one commit", func(v, med float64) string {
		return fmt.Sprintf("one commit adds %s of the code (promo median %s)", pct(v), pct(med))
	})
	features := []*feature{fComments, fSentences, fDocs, fGuards, fIdent, fRare, fFew, fBig}

	// Constructs: keywords, operators and library calls (called, not defined).
	constructs := map[string]map[string]bool{}
	cdf := map[string]int{}
	for _, r := range rs {
		defined := map[string]bool{}
		for _, f := range r.Funcs {
			defined[f.Name] = true
		}
		cs := map[string]bool{}
		for _, f := range r.Funcs {
			for _, t := range f.Body {
				if t.kind == tKeyword || t.kind == tPunct && len(t.text) > 1 {
					cs[t.text] = true
				}
			}
			for _, c := range f.Calls {
				if !defined[c] {
					cs[c+"()"] = true
				}
			}
		}
		constructs[r.Login] = cs
		for c := range cs {
			cdf[c]++
		}
	}
	rareMax := max(1, n/20)

	for _, r := range rs {
		l := r.Login
		lines := commentLines(r.Comments)
		if total := r.CodeLines + r.CommentLines; total > 0 {
			fComments.values[l] = float64(r.CommentLines) / float64(total)
		}
		fSentences.values[l] = 0
		if len(lines) > 0 {
			s := 0
			for _, x := range lines {
				if sentence(x) {
					s++
				}
			}
			fSentences.values[l] = float64(s) / float64(len(lines))
		}
		if len(r.Funcs) > 0 {
			d, g, idLen, ids := 0, 0, 0, 0
			for i := range r.Funcs {
				f := &r.Funcs[i]
				if docTagRe.MatchString(f.Doc) {
					d++
				}
				if guard(f) {
					g++
				}
				for _, id := range f.Idents {
					idLen += len(id)
					ids++
				}
			}
			fDocs.values[l] = float64(d) / float64(len(r.Funcs))
			fGuards.values[l] = float64(g) / float64(len(r.Funcs))
			if ids > 0 {
				fIdent.values[l] = float64(idLen) / float64(ids)
			}
		}
		if n >= 2*MinBaseline {
			rare := 0
			for c := range constructs[l] {
				if cdf[c] <= rareMax {
					rare++
				}
			}
			fRare.values[l] = float64(rare)
		}
		if h, ok := history[l]; ok && len(h) > 0 {
			fFew.values[l] = -float64(len(h))
			total, biggest := 0, 0
			for _, c := range h {
				total += c.Added
				biggest = max(biggest, c.Added)
			}
			if total >= 50 {
				fBig.values[l] = float64(biggest) / float64(total)
			}
		}
	}

	type stat struct{ mean, sd, med float64 }
	stats := map[*feature]stat{}
	for _, f := range features {
		var vs []float64
		for _, v := range f.values {
			vs = append(vs, v)
		}
		if len(vs) < MinBaseline {
			continue
		}
		sort.Float64s(vs)
		var s stat
		for _, v := range vs {
			s.mean += v
		}
		s.mean /= float64(len(vs))
		for _, v := range vs {
			s.sd += (v - s.mean) * (v - s.mean)
		}
		s.sd = math.Sqrt(s.sd / float64(len(vs)))
		s.med = vs[len(vs)/2]
		stats[f] = s
	}

	out := map[string]AIScore{}
	for _, r := range rs {
		l := r.Login
		var sc AIScore
		var zs []float64
		for _, f := range features {
			s, ok := stats[f]
			v, has := f.values[l]
			if !ok || !has || s.sd < 1e-9 {
				continue
			}
			z := (v - s.mean) / s.sd
			if z > 0 {
				zs = append(zs, math.Min(z, 4))
			}
			if z >= 1.5 {
				sc.Signals = append(sc.Signals, Signal{Name: f.name, Detail: f.detail(v, s.med), Z: z})
			}
		}
		sort.Sort(sort.Reverse(sort.Float64Slice(zs)))
		if len(zs) > 0 {
			top := zs[:min(3, len(zs))]
			sum := 0.0
			for _, z := range top {
				sum += z
			}
			sc.Style = squash(sum/3, 2)
		}

		// Absolute tells, meaningful even without a class baseline.
		tells := 0.0
		if t := countTypo(r.Comments) + countTypo(r.Strings); t >= 2 {
			tells++
			sc.Signals = append(sc.Signals, Signal{Name: "typography", Detail: fmt.Sprintf("%d typographic characters (— → ’ … emojis) in comments and strings", t)})
		}
		if v := fDocs.values[l]; len(r.Funcs) >= 3 && v >= 0.5 {
			tells++
			if !hasSignal(sc.Signals, fDocs.name) {
				sc.Signals = append(sc.Signals, Signal{Name: fDocs.name, Detail: fmt.Sprintf("%s of functions have a @param / Args: style doc comment", pct(v))})
			}
		}
		var agent *Signal
		if h := history[l]; len(h) > 0 {
			byTool := map[string][]string{}
			var tools []string
			for _, c := range h {
				if t := agentOf(c); t != "" {
					if len(byTool[t]) == 0 {
						tools = append(tools, t)
					}
					byTool[t] = append(byTool[t], c.Hash[:min(7, len(c.Hash))])
				}
			}
			if len(tools) > 0 {
				sc.Agent = strings.Join(tools, ", ")
				var parts []string
				for _, t := range tools {
					hs := byTool[t]
					ex := strings.Join(hs[:min(3, len(hs))], " ")
					if len(hs) > 3 {
						ex += " …"
					}
					parts = append(parts, fmt.Sprintf("%s in %s (%s)", t, plural(len(hs), "commit", "commits"), ex))
				}
				agent = &Signal{Name: "commits credit " + sc.Agent, Detail: fmt.Sprintf("the history credits an AI tool (co-author, \"Generated with\", author): %s, out of %d commits",
					strings.Join(parts, "; "), len(h))}
			}
		}
		if h := history[l]; len(h) > 0 {
			sc.Commits = len(h)
			total, biggest := 0, 0
			ai := 0
			for _, c := range h {
				total += c.Added
				biggest = max(biggest, c.Added)
				if commitAIRe.MatchString(c.Subject) || commitVerb.MatchString(c.Subject) && len(strings.Fields(c.Subject)) >= 4 {
					ai++
				}
			}
			if total >= 150 && float64(biggest) >= 0.8*float64(total) {
				tells += 0.5
				if !hasSignal(sc.Signals, fBig.name) {
					sc.Signals = append(sc.Signals, Signal{Name: fBig.name, Detail: fmt.Sprintf("one commit adds %d of the %d lines of code", biggest, total)})
				}
			}
			if ai > 0 && 2*ai >= len(h) {
				tells++
				sc.Signals = append(sc.Signals, Signal{Name: "commit messages", Detail: fmt.Sprintf("%d of %d commit messages read like generated ones (\"Implement …\", \"feat: …\", \"robust\")", ai, len(h))})
			}
		}
		sc.Tells = squash(tells, 1.5)
		sc.Score = 1 - (1-sc.Style)*(1-sc.Tells)
		sort.SliceStable(sc.Signals, func(i, j int) bool { return sc.Signals[i].Z > sc.Signals[j].Z })
		if agent != nil {
			sc.Score = math.Max(sc.Score, AgentScore)
			sc.Signals = append([]Signal{*agent}, sc.Signals...)
		}
		sc.TopFuncs = topFuncs(r, stats[fIdent].mean+stats[fIdent].sd)
		out[l] = sc
	}
	return out
}

func hasSignal(ss []Signal, name string) bool {
	for _, s := range ss {
		if s.Name == name {
			return true
		}
	}
	return false
}

// topFuncs returns the functions of a repo that look the most generated.
func topFuncs(r *Repo, longIdent float64) []FuncFlag {
	var out []FuncFlag
	for i := range r.Funcs {
		f := &r.Funcs[i]
		var ff FuncFlag
		if docTagRe.MatchString(f.Doc) {
			ff.Score++
			ff.Why = append(ff.Why, "structured doc comment")
		}
		s := 0
		for _, l := range commentLines(f.Comments) {
			if sentence(l) {
				s++
			}
		}
		if s >= 2 {
			ff.Score++
			ff.Why = append(ff.Why, plural(s, "sentence comment", "sentence comments"))
		}
		if t := countTypo(f.Comments) + countTypo([]string{f.Raw}); t > 0 {
			ff.Score++
			ff.Why = append(ff.Why, "typographic characters")
		}
		if guard(f) {
			ff.Score += 0.5
			ff.Why = append(ff.Why, "guard clause")
		}
		if longIdent > 0 && len(f.Idents) > 0 {
			tot := 0
			for _, id := range f.Idents {
				tot += len(id)
			}
			if float64(tot)/float64(len(f.Idents)) > longIdent {
				ff.Score += 0.5
				ff.Why = append(ff.Why, "long names")
			}
		}
		if ff.Score >= 1 {
			ff.Func = f
			out = append(out, ff)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out[:min(len(out), 8)]
}
