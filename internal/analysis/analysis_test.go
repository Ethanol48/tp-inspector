package analysis

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"tp-inspector/internal/data"
	"tp-inspector/internal/forge"
	"tp-inspector/internal/repos"
)

func TestLevelOf(t *testing.T) {
	for _, c := range []struct {
		score float64
		want  Level
	}{{0.2, Low}, {0.49, Low}, {0.5, Medium}, {0.79, Medium}, {0.8, High}, {1, High}} {
		if got := LevelOf(c.score); got != c.want {
			t.Errorf("LevelOf(%v) = %v, want %v", c.score, got, c.want)
		}
	}
}

var open = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

// student builds a student from chronological (hours after open, percent).
func student(login string, steps ...[2]int) *data.Student {
	st := &data.Student{Login: login, Picked: map[string]*forge.PickedSubmission{}}
	for i, s := range steps {
		p := s[1]
		st.Subs = append([]forge.Submission{{
			ID: fmt.Sprint(login, i), SubmissionDefinitionURI: "a/submit",
			ReceivedAt: open.Add(time.Duration(s[0]) * time.Hour),
			CurrentJob: &forge.Job{SuccessPercent: &p},
		}}, st.Subs...)
	}
	return st
}

func TestRisk(t *testing.T) {
	week := &forge.SubmissionWindow{OpeningAt: open, ClosingAt: open.Add(7 * 24 * time.Hour)}
	students := []*data.Student{
		student("early", [2]int{1, 100}),                                    // 100% one hour after opening, at once
		student("mid", [2]int{30, 40}, [2]int{60, 70}, [2]int{90, 100}),     // gradual work mid-window
		student("lastmin", [2]int{20, 20}, [2]int{167, 100}),                // 20% → 100% just before closing
		student("weak", [2]int{80, 20}, [2]int{167, 40}),                    // late but low grade
		student("jumper", [2]int{50, 10}, [2]int{70, 100}),                  // jump mid-window
		student("steady", [2]int{40, 50}, [2]int{60, 70}, [2]int{100, 100}), // steady
	}
	check := func(r map[string]RiskScore) {
		t.Helper()
		if !(r["early"].Score > r["mid"].Score && r["mid"].Score > r["weak"].Score) {
			t.Errorf("early %.2f > mid %.2f > weak %.2f expected", r["early"].Score, r["mid"].Score, r["weak"].Score)
		}
		if r["lastmin"].Level() != High || r["early"].Level() != High {
			t.Errorf("early and lastmin should be high: %+v %+v", r["early"], r["lastmin"])
		}
		if r["jumper"].Score <= r["steady"].Score {
			t.Errorf("jumper %.2f should beat steady %.2f", r["jumper"].Score, r["steady"].Score)
		}
		if r["weak"].Level() != Low {
			t.Errorf("a low grade is never risky: %+v", r["weak"])
		}
	}
	r := Risk(students, "submit", WindowFor(students, "submit", week))
	check(r)
	if len(r["early"].Reasons) == 0 || !strings.Contains(strings.Join(r["lastmin"].Reasons, ";"), "before the closing") {
		t.Errorf("reasons: early %q lastmin %q", r["early"].Reasons, r["lastmin"].Reasons)
	}
	// Without the API window, the class span stands in for it.
	w := WindowFor(students, "submit", nil)
	if !w.FromClass || !w.Open.Equal(open.Add(time.Hour)) {
		t.Fatalf("class window: %+v", w)
	}
	r = Risk(students, "submit", w)
	if r["lastmin"].Level() != High || r["early"].Score <= r["mid"].Score {
		t.Errorf("fallback: early %+v mid %+v lastmin %+v", r["early"], r["mid"], r["lastmin"])
	}
}

const cSrc = `#include <stdio.h>
/* a struct */
struct node { int v; struct node *next; };

static int helper(int x);

// sums the list
int sum(struct node *n)
{
    int s = 0;
    const char *str = "{ not a brace";
    while (n) { s += n->v; n = n->next; }
    if (s > 0) { s--; }
    return s;
}

#define MAX(a, b) ((a) > (b) ? (a) : (b))

int main(void) { return sum(NULL); }
`

func TestExtractC(t *testing.T) {
	fs := ExtractFuncs("list.c", cSrc)
	if len(fs) != 2 || fs[0].Name != "sum" || fs[1].Name != "main" {
		t.Fatalf("funcs: %+v", names(fs))
	}
	if fs[0].Start != 8 || fs[0].End != 15 {
		t.Errorf("sum spans %d-%d, want 8-15", fs[0].Start, fs[0].End)
	}
	if !strings.HasPrefix(fs[0].Raw, "// sums the list") || len(fs[0].Comments) != 1 {
		t.Errorf("doc comment not attached: %q %q", fs[0].Raw, fs[0].Comments)
	}
	// Only the body is compared: "int s = 0;", not "int sum(struct node *n)".
	if strings.Join(fs[0].Norm[:5], " ") != "int I = N ;" || strings.Join(fs[0].Exact[:3], " ") != "int s =" {
		t.Errorf("norm %v exact %v", fs[0].Norm[:5], fs[0].Exact[:3])
	}
}

const pySrc = `import os

def top(a, b):
    """Add things.

    Args:
        a: first
    """
    def inner(x):
        return x
    return inner(a) + b


class K:
    @staticmethod
    def method(self, s="def fake():"):
        # comment
        if not s:
            raise ValueError("empty")
        return s

async def coro():
    pass
x = 1
`

func TestExtractPython(t *testing.T) {
	fs := ExtractFuncs("m.py", pySrc)
	if got := names(fs); strings.Join(got, ",") != "top,method,coro" {
		t.Fatalf("funcs: %v", got)
	}
	if fs[0].Start != 3 || fs[0].End != 11 || !strings.Contains(fs[0].Doc, "Args:") {
		t.Errorf("top: %d-%d doc %q", fs[0].Start, fs[0].End, fs[0].Doc)
	}
	if fs[1].Start != 15 || fs[1].End != 20 || !strings.HasPrefix(strings.TrimSpace(fs[1].Raw), "@staticmethod") {
		t.Errorf("method: %d-%d %q", fs[1].Start, fs[1].End, fs[1].Raw)
	}
	if !guard(&fs[1]) || guard(&fs[0]) {
		t.Errorf("guard detection wrong")
	}
	if fs[2].End != 23 {
		t.Errorf("coro ends at %d", fs[2].End)
	}
}

func names(fs []Func) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

// skeleton is given to every student.
const skeleton = `
int skeleton_print(int *arr, int len)
{
    for (int i = 0; i < len; i++)
        printf("%d ", arr[i]);
    printf("\n");
    return len;
}
`

const original = `
int find_max(int *values, int count)
{
    int best = values[0];
    for (int k = 1; k < count; k++)
    {
        if (values[k] > best)
            best = values[k];
    }
    while (count > 100) { count = count / 2; best = best + count; }
    return best * 2 + count - 1;
}

void reverse(int *a, int n)
{
    for (int i = 0, j = n - 1; i < j; i++, j--)
    {
        int t = a[i];
        a[i] = a[j];
        a[j] = t;
        if (a[i] == 42) { a[i] = a[i] * 3 + 7; }
    }
}
`

// renamed is original with other names and layout.
const renamed = `
int my_maximum(int *tab, int size) {
    int m = tab[0];
    for (int x = 1; x < size; x++) {
        if (tab[x] > m) m = tab[x]; }
    while (size > 100) { size = size / 2; m = m + size; }
    return m * 2 + size - 1;
}
void rev(int *arr, int len) {
    for (int a = 0, b = len - 1; a < b; a++, b--) {
        int tmp = arr[a]; arr[a] = arr[b]; arr[b] = tmp;
        if (arr[a] == 42) { arr[a] = arr[a] * 3 + 7; }
    }
}
`

func writeRepo(t *testing.T, root, login string, files map[string]string) *Repo {
	t.Helper()
	dir := filepath.Join(root, login)
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := LoadRepo(login, dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCompareRepos(t *testing.T) {
	root := t.TempDir()
	var rs []*Repo
	rs = append(rs, writeRepo(t, root, "alice", map[string]string{"src/main.c": skeleton + original}))
	rs = append(rs, writeRepo(t, root, "bob", map[string]string{"main.c": skeleton + renamed}))
	// Others write their own code with the same skeleton.
	others := []string{
		"int f1(int a, int b) { if (a > b) return a - b; else { while (b) { a = a * 3; b--; } return a; } }",
		"double g2(double *v, int n) { double s = 0.0; for (int i = 0; i < n; i += 2) s += v[i] / 2.0; return s / n; }",
		"char *h3(char *s) { char *p = s; while (*p) { if (*p == ' ') *p = '_'; p++; } return s; }",
		"long k4(long x) { long r = 1; do { r *= x % 10 + 1; x /= 10; } while (x != 0); return r - x; }",
		"unsigned m5(unsigned u) { unsigned c = 0; for (; u; u >>= 1) c += u & 1u; switch (c) { case 0: return 1; default: return c; } }",
	}
	for i, o := range others {
		rs = append(rs, writeRepo(t, root, fmt.Sprintf("other%d", i), map[string]string{"main.c": skeleton + o}))
	}
	pairs := CompareRepos(rs)
	if len(pairs) != 1 || pairs[0].A != "alice" || pairs[0].B != "bob" {
		for _, p := range pairs {
			t.Logf("%s %s %.2f %s", p.A, p.B, p.Score, p.Reason())
		}
		t.Fatalf("want only alice/bob, got %d pairs", len(pairs))
	}
	p := pairs[0]
	if len(p.Matches) != 2 || p.Sim < 0.8 {
		t.Fatalf("matches: %s (sim %.2f)", p.Reason(), p.Sim)
	}
	for _, m := range p.Matches {
		if m.A.Name == "skeleton_print" {
			t.Fatal("the skeleton must not count")
		}
	}
}

const studentC = `
int count(int *t, int n)
{
    int c = 0;
    for (int i = 0; i < n; i++)
        if (t[i] > 0)
            c++;
    return c;
}
`

const generatedC = `
/**
 * @brief Counts the strictly positive values of an array.
 *
 * @param values The array to inspect → must not be NULL (typographic, on purpose).
 * @param length The number of elements in the array.
 * @return The number of positive values, or -1 on invalid input.
 */
int count_positive_values(const int *values, size_t length)
{
    // Validate the input parameters before processing them.
    if (values == NULL)
        return -1;
    int positive_count = 0;
    // Iterate over every element and count the positive ones.
    for (size_t index = 0; index < length; index++)
    {
        if (values[index] > 0)
            positive_count++;
    }
    return positive_count;
}
`

func TestAIScores(t *testing.T) {
	root := t.TempDir()
	var rs []*Repo
	for i := 0; i < 6; i++ {
		rs = append(rs, writeRepo(t, root, fmt.Sprintf("s%d", i), map[string]string{"a.c": studentC}))
	}
	rs = append(rs, writeRepo(t, root, "gen", map[string]string{"a.c": generatedC + strings.ReplaceAll(generatedC, "count_positive", "count_negative")}))
	hist := map[string][]repos.Commit{}
	for _, r := range rs {
		hist[r.Login] = []repos.Commit{{Subject: "tp", Added: 20}, {Subject: "fix", Added: 5}, {Subject: "ex2", Added: 30}}
	}
	hist["gen"] = []repos.Commit{{Subject: "Implement robust counting with edge case handling", Added: 400}}
	scores := AIScores(rs, hist)
	g := scores["gen"]
	if g.Level() != High {
		t.Errorf("generated repo should be high: %.2f %+v", g.Score, g.Signals)
	}
	for _, r := range rs[:6] {
		if s := scores[r.Login]; s.Score >= 0.5 {
			t.Errorf("%s should be low: %.2f %+v", r.Login, s.Score, s.Signals)
		}
	}
	if len(g.TopFuncs) == 0 || !strings.HasPrefix(g.TopFuncs[0].Func.Name, "count_") {
		t.Errorf("top funcs: %+v", g.TopFuncs)
	}
	var sig []string
	for _, s := range g.Signals {
		sig = append(sig, s.Name)
	}
	sort.Strings(sig)
	for _, want := range []string{"commit messages", "doc comments", "typography"} {
		if i := sort.SearchStrings(sig, want); i == len(sig) || sig[i] != want {
			t.Errorf("missing signal %q in %v", want, sig)
		}
	}
}

func TestRiskBinary(t *testing.T) {
	// archi-like: everyone 0% then 100%, no jump signal.
	var sts []*data.Student
	for i := 0; i < 10; i++ {
		sts = append(sts, student(fmt.Sprint("s", i), [2]int{50, 0}, [2]int{80, 100}))
	}
	win := &forge.SubmissionWindow{OpeningAt: open, ClosingAt: open.Add(7 * 24 * time.Hour)}
	for l, r := range Risk(sts, "submit", WindowFor(sts, "submit", win)) {
		if r.Jump != 0 || r.Level() != Low {
			t.Errorf("%s: %+v", l, r)
		}
	}
}

func TestAgentOf(t *testing.T) {
	for _, c := range []struct {
		commit repos.Commit
		want   string
	}{
		{repos.Commit{Subject: "ex1", Body: "Co-Authored-By: Claude <noreply@anthropic.com>"}, "Claude"},
		{repos.Commit{Subject: "ex1", Body: "Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"}, "Claude"},
		{repos.Commit{Subject: "ex1", Body: "🤖 Generated with [Claude Code](https://claude.com/claude-code)"}, "Claude"},
		{repos.Commit{Subject: "fix", Author: "copilot-swe-agent[bot] <198982749+Copilot@users.noreply.github.com>"}, "Copilot"},
		{repos.Commit{Subject: "fix", Body: "Co-authored-by: Cursor Agent <cursoragent@cursor.com>"}, "Cursor"},
		{repos.Commit{Subject: "fix", Author: "Jean Dupont (aider) <jean@epita.fr>"}, "Aider"},
		{repos.Commit{Subject: "feat: add list", Body: "Co-authored-by: Codex <codex@openai.com>"}, "ChatGPT / Codex"},
		// A student called Claude, and ordinary messages, are not tools.
		{repos.Commit{Subject: "tp", Author: "Claude Martin <claude.martin@epita.fr>"}, ""},
		{repos.Commit{Subject: "pair work", Body: "Co-authored-by: Claude Martin <claude.martin@epita.fr>"}, ""},
		{repos.Commit{Subject: "move the cursor in the copilot game"}, ""},
		{repos.Commit{Subject: "numbers generated by rand"}, ""},
	} {
		if got := agentOf(c.commit); got != c.want {
			t.Errorf("agentOf(%+v) = %q, want %q", c.commit, got, c.want)
		}
	}
}

func TestAIScoresAgent(t *testing.T) {
	root := t.TempDir()
	var rs []*Repo
	hist := map[string][]repos.Commit{}
	for i := 0; i < 6; i++ {
		l := fmt.Sprintf("s%d", i)
		rs = append(rs, writeRepo(t, root, l, map[string]string{"a.c": studentC}))
		hist[l] = []repos.Commit{{Hash: "aaaaaaaa", Subject: "tp", Added: 20}, {Hash: "bbbbbbbb", Subject: "fix", Added: 5}}
	}
	// Student-looking code, but the history credits Claude.
	hist["s0"][1].Body = "Co-Authored-By: Claude <noreply@anthropic.com>"
	s := AIScores(rs, hist)["s0"]
	if s.Score < AgentScore || s.Agent != "Claude" || len(s.Signals) == 0 || s.Signals[0].Name != "commits credit Claude" ||
		!strings.Contains(s.Signals[0].Detail, "bbbbbbb") {
		t.Fatalf("credited AI not flagged first: %+v", s)
	}
	if o := AIScores(rs, hist)["s1"]; o.Score >= 0.5 || o.Agent != "" {
		t.Fatalf("s1 should stay low: %+v", o)
	}
}

// Everyone frees a list the same way; two students share one version as
// written, only tabs and spaces differ. The normalized shape is common, the
// exact text is not.
func TestCompareReposIdentical(t *testing.T) {
	impl := func(name, cur, nxt string) string {
		return fmt.Sprintf(`
void free_all(struct node *head)
{
    struct node *%[1]s = head;
    while (%[1]s != NULL)
    {
        struct node *%[2]s = %[1]s->next;
        free(%[1]s->data);
        free(%[1]s);
        %[1]s = %[2]s;
    }
}

int %[3]s(struct node *head)
{
    int count = 0;
    for (struct node *%[1]s = head; %[1]s != NULL; %[1]s = %[1]s->next)
        if (%[1]s->data != NULL)
            count++;
    return count;
}
`, cur, nxt, name)
	}
	copied := impl("count_full", "cursor", "following")
	tabbed := strings.NewReplacer("    ", "\t", " = ", "=", " != ", "!=").Replace(copied)
	root := t.TempDir()
	rs := []*Repo{
		writeRepo(t, root, "alice", map[string]string{"list.c": copied}),
		writeRepo(t, root, "bob", map[string]string{"list.c": tabbed}),
	}
	names := [][2]string{{"cur", "next_node"}, {"it", "tmp"}, {"node", "after"}, {"p", "q"}, {"elt", "nxt"}}
	for i, nm := range names {
		rs = append(rs, writeRepo(t, root, fmt.Sprint("other", i), map[string]string{"list.c": impl("count_full", nm[0], nm[1])}))
	}
	pairs := CompareRepos(rs)
	if len(pairs) == 0 || pairs[0].A != "alice" || pairs[0].B != "bob" {
		t.Fatalf("alice/bob must come first: %+v", pairs)
	}
	p := pairs[0]
	if p.Same != 2 || !p.Matches[0].Identical {
		t.Fatalf("identical copy not recognized: score %.2f, %s", p.Score, p.Reason())
	}
	for _, o := range pairs[1:] {
		if o.Score >= p.Score {
			t.Errorf("%s/%s (renamed, common shape) %.2f should be below the copy %.2f", o.A, o.B, o.Score, p.Score)
		}
	}
}

// The subject gives everyone the same names and parameters: only bodies
// count. Same signatures with different bodies are not alike, and the one
// possible short body is not a copy.
func TestCompareReposBodiesOnly(t *testing.T) {
	sig := "int insert_contact(struct contact_book *book, const char *name, const char *phone_number, unsigned int age)\n"
	getter := "unsigned int contact_age(const struct contact_book *book, const struct contact *contact)\n{\n    return contact->age;\n}\n"
	bodies := []string{
		"{\n    struct contact *c = malloc(sizeof(*c));\n    if (!c)\n        return -1;\n    c->name = strdup(name);\n    c->age = age;\n    c->next = book->head;\n    book->head = c;\n    return 0;\n}\n",
		"{\n    int n = 0;\n    while (n < 3 && phone_number[n] != '\\0')\n        n++;\n    if (n != 3 || age > 150)\n        return 1;\n    book->size += 1;\n    printf(\"%s\\n\", name);\n    return 0;\n}\n",
		"{\n    for (unsigned int i = 0; i < age; i++)\n        if (name[i % 4] == phone_number[i % 2])\n            return (int)i;\n    return book == NULL ? -2 : (int)age * 2;\n}\n",
	}
	root := t.TempDir()
	var rs []*Repo
	for i, b := range bodies {
		rs = append(rs, writeRepo(t, root, fmt.Sprint("s", i), map[string]string{"book.c": sig + b + getter}))
	}
	if fs := rs[0].Funcs; len(fs) != 2 || len(fs[1].Exact) >= MinFuncToks {
		t.Fatalf("the getter body is %d tokens, the signature must not count", len(fs[1].Exact))
	}
	if pairs := CompareRepos(rs); len(pairs) != 0 {
		t.Fatalf("same signatures, different bodies, trivial getter: no pair expected, got %s/%s %s", pairs[0].A, pairs[0].B, pairs[0].Reason())
	}
}
