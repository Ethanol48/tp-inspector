// Package config reads the user settings of tpi from a TOML file,
// ~/.config/tpi/config.toml by default ($XDG_CONFIG_HOME is honoured).
//
// Every setting is optional. Command line flags and environment variables
// take precedence over the file, the file over the built-in defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"tp-inspector/internal/forge"
)

// EnvPath names another config file, like --config.
const EnvPath = "TPI_CONFIG"

// Config is the content of the config file. Empty fields keep the default.
type Config struct {
	// CacheDir replaces ~/.cache/tpi (API responses, traces, cloned repos).
	CacheDir string `toml:"cache_dir"`
	// Tenant is the default of --tenant.
	Tenant string `toml:"tenant"`
	// Logins is the default of --logins: a file of logins, your group.
	Logins string `toml:"logins"`

	// Theme is the color theme of the TUI (see `tpi config themes`).
	Theme string `toml:"theme"`
	// Background forces the light or dark variant of the colors: "auto"
	// (default, asks the terminal), "dark" or "light".
	Background string `toml:"background"`
	// Colors overrides colors of the theme, by name (accent, secondary...).
	Colors map[string]Color `toml:"colors"`

	Tokens    Tokens    `toml:"tokens"`
	CRIClient CRIClient `toml:"cri_client"`

	// File is the path the config was read from ("" when there is none).
	File string `toml:"-"`
}

// Tokens are the files where the OAuth2 sessions are stored.
type Tokens struct {
	Operator string `toml:"operator"` // default ~/.cri-tokens, shared with poulpy
	CRIAPI   string `toml:"cri_api"`  // default ~/.cri-api-tokens
}

// CRIClient replaces the OAuth2 client of the CRI API login; the
// TPI_CRI_CLIENT_ID / TPI_CRI_CLIENT_SECRET variables take precedence.
type CRIClient struct {
	ID     string `toml:"id"`
	Secret string `toml:"secret"`
}

// Color is a color of the config: "#RRGGBB", "#RGB" or an ANSI number
// ("0".."255"), the same on light and dark terminals, or a table
// { light = "...", dark = "..." } (a missing side keeps the theme's).
type Color struct {
	Light, Dark string
}

// UnmarshalTOML accepts a string or a { light, dark } table.
func (c *Color) UnmarshalTOML(v any) error {
	switch v := v.(type) {
	case string:
		c.Light, c.Dark = v, v
	case map[string]any:
		for k, x := range v {
			s, ok := x.(string)
			switch {
			case !ok:
				return fmt.Errorf("color %s: want a string, got %v", k, x)
			case k == "light":
				c.Light = s
			case k == "dark":
				c.Dark = s
			default:
				return fmt.Errorf("color: unknown key %q (want light or dark)", k)
			}
		}
	default:
		return fmt.Errorf("color: want \"#RRGGBB\" or { light = ..., dark = ... }, got %v", v)
	}
	for _, s := range []string{c.Light, c.Dark} {
		if s != "" && !ValidColor(s) {
			return fmt.Errorf("color %q: want #RRGGBB, #RGB or 0..255", s)
		}
	}
	return nil
}

// ValidColor reports whether s is "#RRGGBB", "#RGB" or an ANSI number 0..255.
func ValidColor(s string) bool {
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) != 3 && len(h) != 6 {
			return false
		}
		for _, r := range h {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255
}

// DefaultPath is the config file used without --config or $TPI_CONFIG.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "tpi", "config.toml")
}

// Path returns the config file to read: explicit (from --config) if set,
// else $TPI_CONFIG, else DefaultPath. required reports whether the file was
// asked for, in which case a missing file is an error.
func Path(explicit string) (path string, required bool) {
	if explicit != "" {
		return explicit, true
	}
	if p := os.Getenv(EnvPath); p != "" {
		return p, true
	}
	return DefaultPath(), false
}

// Load reads the config file at path. A missing file gives an empty Config,
// unless required. Unknown keys are errors, to catch typos.
func Load(path string, required bool) (Config, error) {
	var c Config
	path = Expand(path, "")
	md, err := toml.DecodeFile(path, &c)
	if errors.Is(err, fs.ErrNotExist) && !required {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, len(und))
		for i, k := range und {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return Config{}, fmt.Errorf("config %s: unknown key %s", path, strings.Join(keys, ", "))
	}
	c.File = path

	base := filepath.Dir(path)
	for _, p := range []*string{&c.CacheDir, &c.Logins, &c.Tokens.Operator, &c.Tokens.CRIAPI} {
		*p = Expand(*p, base)
	}
	return c, nil
}

// Expand replaces a leading ~ by the home directory and $VAR / ${VAR} by
// their values. A relative result is taken relative to base (if not empty).
func Expand(p, base string) string {
	if p == "" {
		return ""
	}
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if base != "" && !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// Template is a commented config file with every setting, written by
// `tpi config init`.
const Template = `# tpi configuration. Every setting is optional: remove the "#" to set one.
# Command line flags and environment variables take precedence.
# Paths accept ~ and $VARIABLES; relative paths are relative to this file.

# Cache of API responses, traces and cloned repos (--cache).
# cache_dir = "~/.cache/tpi"

# Default tenant of the activity (--tenant). By default it is looked up.
# tenant = "epita-prepa-computer-science"

# Default file of logins, your group (--logins).
# logins = "~/my-group.txt"

# Color theme of the TUI: tpi, claude, catppuccin, nord, gruvbox,
# tokyonight or mono. Preview them with ` + "`tpi config themes`" + `.
# theme = "tpi"

# Light or dark variant of the colors: auto (asks the terminal), dark, light.
# background = "auto"

[colors]
# Override colors of the theme: one color for both backgrounds, or
# { light = "...", dark = "..." }. Colors are "#RRGGBB", "#RGB" or 0..255.
#   accent          title, active tab, selection
#   secondary       keys in the hints, section headers
#   text_on_accent  text of the active tab
#   dim, faint      secondary text; borders and empty bar cells
#   good, warn, bad pass rates and risk levels
#   highlight       background of the selected row
# accent = "#A78BFA"
# secondary = { light = "#0F8A7E", dark = "#5EEAD4" }

[tokens]
# Operator session, shared with poulpy.
# operator = "~/.cri-tokens"
# CRI API session (classes, F and --select-group).
# cri_api = "~/.cri-api-tokens"

[cri_client]
# OAuth2 client of the CRI API login; it must allow the redirect URI
# http://localhost:8080/complete/epita/. TPI_CRI_CLIENT_ID and
# TPI_CRI_CLIENT_SECRET take precedence. Default: the Forge ID public test
# client (development only).
# id = "` + forge.TestClientID + `"
# secret = "` + forge.TestClientSecret + `"
`
