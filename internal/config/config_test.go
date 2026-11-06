package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("TPI_TEST_DIR", "/srv/tpi")
	p := write(t, `
cache_dir = "$TPI_TEST_DIR/cache"
logins = "group.txt"
tenant = "epita-prepa-computer-science"
[tokens]
operator = "~/tok"
[cri_client]
id = "42"
`)
	c, err := Load(p, true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"cache_dir":       "/srv/tpi/cache",
		"logins":          filepath.Join(filepath.Dir(p), "group.txt"),
		"tenant":          "epita-prepa-computer-science",
		"tokens.operator": filepath.Join(home, "tok"),
		"tokens.cri_api":  "",
		"cri_client.id":   "42",
		"file":            p,
	}
	got := map[string]string{
		"cache_dir": c.CacheDir, "logins": c.Logins, "tenant": c.Tenant,
		"tokens.operator": c.Tokens.Operator, "tokens.cri_api": c.Tokens.CRIAPI,
		"cri_client.id": c.CRIClient.ID, "file": c.File,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "none.toml")
	if c, err := Load(p, false); err != nil || c != (Config{}) {
		t.Errorf("optional missing file: %+v, %v", c, err)
	}
	if _, err := Load(p, true); err == nil {
		t.Error("required missing file: no error")
	}
}

func TestLoadUnknownKey(t *testing.T) {
	_, err := Load(write(t, "cache_dri = \"x\"\n"), false)
	if err == nil || !strings.Contains(err.Error(), "cache_dri") {
		t.Errorf("err = %v, want unknown key cache_dri", err)
	}
}

func TestTemplateParses(t *testing.T) {
	if _, err := Load(write(t, Template), true); err != nil {
		t.Error(err)
	}
}
