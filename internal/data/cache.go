package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Cache stores API responses and traces under ~/.cache/tpi.
type Cache struct {
	Root    string
	Refresh bool // ignore cached JSON (traces are immutable and always reused)
}

func DefaultCacheRoot() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "tpi")
}

func (c *Cache) path(parts ...string) string {
	return filepath.Join(append([]string{c.Root}, parts...)...)
}

// ReadJSON loads a cached value and returns its modification time.
func (c *Cache) ReadJSON(name string, v any) (time.Time, bool) {
	if c.Refresh {
		return time.Time{}, false
	}
	p := c.path(name)
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, v) != nil {
		return time.Time{}, false
	}
	st, _ := os.Stat(p)
	return st.ModTime(), true
}

func (c *Cache) WriteJSON(name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.WriteFile(name, b)
}

func (c *Cache) ReadFile(name string) ([]byte, bool) {
	b, err := os.ReadFile(c.path(name))
	return b, err == nil
}

func (c *Cache) WriteFile(name string, b []byte) error {
	p := c.path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
