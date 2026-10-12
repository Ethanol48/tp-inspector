package repos

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLog(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		if _, err := git(ctx, dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write("main.c", strings.Repeat("int x;\n", 100))
	write("notes.txt", strings.Repeat("x\n", 50))
	run("add", ".")
	run("commit", "-q", "-m", "Implement everything")
	write("main.c", strings.Repeat("int x;\n", 103))
	run("commit", "-q", "-am", "fix", "-m", "Co-Authored-By: Claude <noreply@anthropic.com>")

	cs, err := Log(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Subject != "Implement everything" || cs[0].Added != 100 || cs[1].Added != 3 ||
		cs[1].Subject != "fix" || cs[1].Body != "Co-Authored-By: Claude <noreply@anthropic.com>" || cs[0].Author != "t <t@t>" {
		t.Fatalf("log: %+v", cs)
	}
}
