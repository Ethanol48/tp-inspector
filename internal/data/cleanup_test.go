package data

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCleanup(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "activities.json"))
	write(t, filepath.Join(root, "tp-a", "groups.json"))
	write(t, filepath.Join(root, "tp-a", "traces", "1.xml"))
	write(t, filepath.Join(root, "tp-a", "repos", "alice", "main.c"))
	write(t, filepath.Join(root, "tp-a", "repos", "bob", "main.c"))
	write(t, filepath.Join(root, "tp-b", "repos", "carol", "main.c"))

	es, err := ScanCache(root)
	if err != nil || len(es) != 2 || es[0].Traces != 1 || len(es[0].Repos) != 2 {
		t.Fatalf("scan: %+v %v", es, err)
	}

	if err := RemoveRepo(root, "tp-a", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(RepoDir(root, "tp-a", "bob")); err != nil {
		t.Fatal("bob should remain")
	}
	if err := RemoveRepo(root, "tp-a", "../../escape"); err == nil {
		t.Fatal("path escaping the root must be refused")
	}

	if err := RemoveData(root, "tp-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "tp-a", "groups.json")); !os.IsNotExist(err) {
		t.Fatal("data should be gone")
	}
	if _, err := os.Stat(RepoDir(root, "tp-a", "bob")); err != nil {
		t.Fatal("repos must survive RemoveData")
	}

	if err := RemoveAllRepos(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "activities.json")); err != nil {
		t.Fatal("RemoveAllRepos must keep other data")
	}
	if err := RemoveEverything(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("root should be gone")
	}
}

func TestPickGradedSD(t *testing.T) {
	for _, c := range []struct {
		names []string
		want  string
	}{
		{[]string{"archi", "submit"}, "submit"},         // TP: submit-* runs the testsuite
		{[]string{"submission"}, "submission"},          // exam: submission-*
		{[]string{"archi", "submission"}, "submission"}, // archi-* only checks the layout
		{[]string{"acdc-archi", "acdc-submit"}, "acdc-submit"},
		{[]string{"archi"}, ""},
	} {
		if got := PickGradedSD(c.names); got != c.want {
			t.Errorf("PickGradedSD(%v) = %q, want %q", c.names, got, c.want)
		}
	}
}
