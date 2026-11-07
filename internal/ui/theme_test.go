package ui

import (
	"strings"
	"testing"

	settings "tp-inspector/internal/config"
)

func TestApplyConfig(t *testing.T) {
	defer setTheme(Themes[DefaultTheme])
	err := ApplyConfig(settings.Config{Theme: "nord", Colors: map[string]settings.Color{
		"accent": {Light: "#111111", Dark: "#222222"},
		"bad":    {Dark: "#333333"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	nord := Themes["nord"]
	if cAccent.Light != "#111111" || cAccent.Dark != "#222222" {
		t.Errorf("accent = %+v", cAccent)
	}
	if cBad.Dark != "#333333" || cBad.Light != nord.Bad.Light {
		t.Errorf("bad = %+v, want light kept from nord", cBad)
	}
	if cGood != nord.Good {
		t.Errorf("good = %+v, want nord's", cGood)
	}
	if Themes["nord"].Accent != nord.Accent {
		t.Error("overrides modified the built-in theme")
	}
}

func TestApplyConfigErrors(t *testing.T) {
	defer setTheme(Themes[DefaultTheme])
	for _, c := range []settings.Config{
		{Theme: "nope"},
		{Colors: map[string]settings.Color{"acent": {Dark: "#fff"}}},
		{Background: "grey"},
	} {
		if err := ApplyConfig(c); err == nil {
			t.Errorf("%+v: no error", c)
		}
	}
}

func TestThemePreviewRestores(t *testing.T) {
	defer setTheme(Themes[DefaultTheme])
	before := cAccent
	for _, n := range ThemeNames() {
		if strings.TrimSpace(ThemePreview(n)) == "" {
			t.Errorf("%s: empty preview", n)
		}
	}
	if cAccent != before {
		t.Error("ThemePreview changed the current theme")
	}
}
