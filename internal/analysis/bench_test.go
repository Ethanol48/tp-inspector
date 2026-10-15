package analysis

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var varsN = 8

// genFunc writes a plausible student function from a small statement grammar.
func genFunc(r *rand.Rand, name string) string {
	vars := []string{"i", "j", "n", "len", "res", "tmp", "count", "x"}[:varsN]
	v := func() string { return vars[r.Intn(len(vars))] }
	ops := []string{"+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>"}
	cmp := []string{"<", ">", "<=", ">=", "==", "!="}
	o := func() string { return ops[r.Intn(len(ops))] }
	c := func() string { return cmp[r.Intn(len(cmp))] }
	stmts := []func() string{
		func() string { return fmt.Sprintf("%s = %s %s %d;", v(), v(), o(), r.Intn(5)) },
		func() string {
			return fmt.Sprintf("for (int %s = 0; %s %s %s; %s++) { %s %s= %s; }", v(), v(), c(), v(), v(), v(), o(), v())
		},
		func() string {
			return fmt.Sprintf("if (%s %s %s) { %s = %s %s 1; } else { %s--; }", v(), c(), v(), v(), v(), o(), v())
		},
		func() string { return fmt.Sprintf("while (%s %s 0) { %s = %s %s 2; }", v(), c(), v(), v(), o()) },
		func() string { return fmt.Sprintf("%s = arr[%s] %s %s;", v(), v(), o(), v()) },
		func() string { return fmt.Sprintf("printf(\"%%d\\n\", %s);", v()) },
	}
	var b strings.Builder
	fmt.Fprintf(&b, "int %s(int *arr, int n)\n{\n    int i = 0, j = 0, len = n, res = 0, tmp = 0, count = 0, x = 0;\n", name)
	for k := 0; k < 6+r.Intn(10); k++ {
		b.WriteString("    ")
		b.WriteString(stmts[r.Intn(len(stmts))]())
		b.WriteString("\n")
	}
	b.WriteString("    return res;\n}\n")
	return b.String()
}

// BenchmarkClass compares a synthetic promotion. TPI_BENCH_N sets the number
// of repos (600), TPI_BENCH_POOL > 0 draws functions from a shared pool so
// each is found in several repos, the costliest case for the pair counting.
func BenchmarkClass(b *testing.B) {
	n := 600
	if s := os.Getenv("TPI_BENCH_N"); s != "" {
		fmt.Sscan(s, &n)
	}
	if s := os.Getenv("TPI_BENCH_VARS"); s != "" {
		fmt.Sscan(s, &varsN)
	}
	root := b.TempDir()
	r := rand.New(rand.NewSource(1))
	var pool []string
	if s := os.Getenv("TPI_BENCH_POOL"); s != "" {
		var k int
		fmt.Sscan(s, &k)
		for i := 0; i < k; i++ {
			pool = append(pool, genFunc(r, fmt.Sprint("p", i)))
		}
	}
	var rs []*Repo
	for i := 0; i < n; i++ {
		var src strings.Builder
		src.WriteString(skeleton)
		for f := range 25 {
			if len(pool) > 0 {
				src.WriteString(pool[r.Intn(len(pool))])
			} else {
				src.WriteString(genFunc(r, fmt.Sprintf("ex%d", f)))
			}
		}
		dir := filepath.Join(root, fmt.Sprint(i))
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "main.c"), []byte(src.String()), 0o644)
		repo, _ := LoadRepo(fmt.Sprint(i), dir)
		rs = append(rs, repo)
	}

	for b.Loop() {
		t := time.Now()
		pairs := CompareRepos(rs)
		ai := AIScores(rs, nil)
		lv := map[Level]int{}
		for _, p := range pairs {
			lv[p.Level()]++
		}
		b.Logf("%d repos: %d pairs %v, %d ai in %v", n, len(pairs), lv, len(ai), time.Since(t))
		if len(pairs) > 0 {
			p := pairs[0]
			b.Logf("top: %.2f sim %.2f matched %d %s", p.Score, p.Sim, p.Matched, p.Reason())
		}
	}
}
