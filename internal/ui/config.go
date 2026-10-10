package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// config holds UI preferences persisted across runs.
type config struct {
	HiddenColumns map[string][]string `json:"hidden_columns"` // view -> column ids
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "tpi", "config.json")
}

func loadConfig() config {
	var c config
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.HiddenColumns == nil {
		c.HiddenColumns = map[string][]string{}
	}
	return c
}

func (c config) save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
