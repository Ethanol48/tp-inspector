package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	settings "tp-inspector/internal/config"
)

// Theme is the palette of the TUI. Each color has a light and a dark
// variant, picked from the terminal background.
type Theme struct {
	Accent    lipgloss.AdaptiveColor // title, active tab, selection
	Secondary lipgloss.AdaptiveColor // keys in the hints, section headers
	OnAccent  lipgloss.AdaptiveColor // text of the active tab
	Dim       lipgloss.AdaptiveColor // secondary text
	Faint     lipgloss.AdaptiveColor // borders, empty bar cells
	Good      lipgloss.AdaptiveColor
	Warn      lipgloss.AdaptiveColor
	Bad       lipgloss.AdaptiveColor
	Highlight lipgloss.AdaptiveColor // background of the selected row
}

func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

// DefaultTheme is used when the config sets none.
const DefaultTheme = "tpi"

// Themes are the built-in themes, by name.
var Themes = map[string]Theme{
	"tpi": { // the colors of the first versions
		Accent:    ac("#C15F3C", "#D97757"),
		Secondary: ac("#C15F3C", "#D97757"),
		OnAccent:  ac("#FFFFFF", "#FFFFFF"),
		Dim:       ac("#707070", "#8A8A8A"),
		Faint:     ac("#D0D0D0", "#3A3A3A"),
		Good:      ac("#2E7D32", "#7BC67E"),
		Warn:      ac("#B26A00", "#E5C07B"),
		Bad:       ac("#C62828", "#EF6B6B"),
		Highlight: ac("#FBE3D9", "#4A2A20"),
	},
	"violet": {
		Accent:    ac("#6D4AE0", "#A78BFA"),
		Secondary: ac("#0F8A7E", "#5EEAD4"),
		OnAccent:  ac("#FFFFFF", "#1A1530"),
		Dim:       ac("#6B6F80", "#8B8FA3"),
		Faint:     ac("#D5D7E0", "#34374A"),
		Good:      ac("#1E8E4E", "#86E1A0"),
		Warn:      ac("#A86A00", "#F5C97A"),
		Bad:       ac("#C6284C", "#F38BA8"),
		Highlight: ac("#EEE8FF", "#2D2547"),
	},
	"catppuccin": { // latte / mocha
		Accent:    ac("#8839EF", "#CBA6F7"),
		Secondary: ac("#04A5E5", "#89DCEB"),
		OnAccent:  ac("#EFF1F5", "#1E1E2E"),
		Dim:       ac("#8C8FA1", "#7F849C"),
		Faint:     ac("#CCD0DA", "#45475A"),
		Good:      ac("#40A02B", "#A6E3A1"),
		Warn:      ac("#DF8E1D", "#F9E2AF"),
		Bad:       ac("#D20F39", "#F38BA8"),
		Highlight: ac("#E6E9EF", "#313244"),
	},
	"nord": {
		Accent:    ac("#5E81AC", "#88C0D0"),
		Secondary: ac("#B48EAD", "#B48EAD"),
		OnAccent:  ac("#ECEFF4", "#2E3440"),
		Dim:       ac("#4C566A", "#7B88A1"),
		Faint:     ac("#D8DEE9", "#3B4252"),
		Good:      ac("#6A8F4E", "#A3BE8C"),
		Warn:      ac("#B5892B", "#EBCB8B"),
		Bad:       ac("#BF616A", "#BF616A"),
		Highlight: ac("#E5E9F0", "#434C5E"),
	},
	"gruvbox": {
		Accent:    ac("#AF3A03", "#FE8019"),
		Secondary: ac("#427B58", "#8EC07C"),
		OnAccent:  ac("#FBF1C7", "#282828"),
		Dim:       ac("#7C6F64", "#A89984"),
		Faint:     ac("#D5C4A1", "#3C3836"),
		Good:      ac("#79740E", "#B8BB26"),
		Warn:      ac("#B57614", "#FABD2F"),
		Bad:       ac("#9D0006", "#FB4934"),
		Highlight: ac("#EBDBB2", "#504945"),
	},
	"tokyonight": {
		Accent:    ac("#2E7DE9", "#7AA2F7"),
		Secondary: ac("#9854F1", "#BB9AF7"),
		OnAccent:  ac("#FFFFFF", "#1A1B26"),
		Dim:       ac("#6172B0", "#737AA2"),
		Faint:     ac("#C4C8DA", "#292E42"),
		Good:      ac("#587539", "#9ECE6A"),
		Warn:      ac("#8C6C3E", "#E0AF68"),
		Bad:       ac("#F52A65", "#F7768E"),
		Highlight: ac("#DCE2F5", "#283457"),
	},
	"mono": { // grays, color only for the pass rates and risks
		Accent:    ac("#202020", "#E0E0E0"),
		Secondary: ac("#505050", "#B0B0B0"),
		OnAccent:  ac("#FFFFFF", "#000000"),
		Dim:       ac("#707070", "#8A8A8A"),
		Faint:     ac("#D0D0D0", "#3A3A3A"),
		Good:      ac("#2E7D32", "#7BC67E"),
		Warn:      ac("#B26A00", "#E5C07B"),
		Bad:       ac("#C62828", "#EF6B6B"),
		Highlight: ac("#E8E8E8", "#303030"),
	},
}

// ThemeNames returns the built-in themes, the default first.
func ThemeNames() []string {
	names := []string{DefaultTheme}
	for n := range Themes {
		if n != DefaultTheme {
			names = append(names, n)
		}
	}
	sort.Strings(names[1:])
	return names
}

// colorNames maps the keys of [colors] in the config to the theme fields.
func (t *Theme) colors() map[string]*lipgloss.AdaptiveColor {
	return map[string]*lipgloss.AdaptiveColor{
		"accent": &t.Accent, "secondary": &t.Secondary, "text_on_accent": &t.OnAccent,
		"dim": &t.Dim, "faint": &t.Faint, "good": &t.Good, "warn": &t.Warn, "bad": &t.Bad,
		"highlight": &t.Highlight,
	}
}

// ColorNames are the keys accepted in [colors], sorted.
func ColorNames() []string {
	var t Theme
	var names []string
	for n := range t.colors() {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ApplyConfig sets the theme, background and color overrides of the config
// for every TUI of the process.
func ApplyConfig(c settings.Config) error {
	name := c.Theme
	if name == "" {
		name = DefaultTheme
	}
	t, ok := Themes[name]
	if !ok {
		return fmt.Errorf("config: unknown theme %q (want %s)", name, strings.Join(ThemeNames(), ", "))
	}
	fields := t.colors()
	for k, v := range c.Colors {
		f, ok := fields[k]
		if !ok {
			return fmt.Errorf("config: unknown color %q in [colors] (want %s)", k, strings.Join(ColorNames(), ", "))
		}
		if v.Light != "" {
			f.Light = v.Light
		}
		if v.Dark != "" {
			f.Dark = v.Dark
		}
	}
	switch c.Background {
	case "", "auto":
	case "dark":
		lipgloss.SetHasDarkBackground(true)
	case "light":
		lipgloss.SetHasDarkBackground(false)
	default:
		return fmt.Errorf("config: background %q: want auto, dark or light", c.Background)
	}
	setTheme(t)
	return nil
}

// ThemePreview renders a sample of a theme on one line: tabs, keys, a bar
// and the status colors.
func ThemePreview(name string) string {
	saved := current
	defer setTheme(saved)
	setTheme(Themes[name])
	return renderTabs([]string{"Overview", "Ranking"}, 0) + "  " +
		sSectionHdr.Render("Tests") + "  " + keyHint("q", "quit") + "  " +
		pctBar(85, 6) + " " + pctCell(85) + " " + pctCell(60) + " " + pctCell(20) + "  " +
		sHighlight.Render(" selected ")
}
