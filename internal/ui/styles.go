package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	cAccent = lipgloss.AdaptiveColor{Light: "#C15F3C", Dark: "#D97757"}
	cDim    = lipgloss.AdaptiveColor{Light: "#707070", Dark: "#8A8A8A"}
	cFaint  = lipgloss.AdaptiveColor{Light: "#D0D0D0", Dark: "#3A3A3A"}
	cGood   = lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#7BC67E"}
	cWarn   = lipgloss.AdaptiveColor{Light: "#B26A00", Dark: "#E5C07B"}
	cBad    = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#EF6B6B"}
	cHiBg   = lipgloss.AdaptiveColor{Light: "#FBE3D9", Dark: "#4A2A20"}

	sTitle      = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sBold       = lipgloss.NewStyle().Bold(true)
	sDim        = lipgloss.NewStyle().Foreground(cDim)
	sAccent     = lipgloss.NewStyle().Foreground(cAccent)
	sGood       = lipgloss.NewStyle().Foreground(cGood)
	sWarn       = lipgloss.NewStyle().Foreground(cWarn)
	sBad        = lipgloss.NewStyle().Foreground(cBad)
	sHeader     = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	sTabActive  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(cAccent).Bold(true).Padding(0, 1)
	sTabIdle    = lipgloss.NewStyle().Foreground(cDim).Padding(0, 1)
	sKey        = lipgloss.NewStyle().Foreground(cAccent)
	sHighlight  = lipgloss.NewStyle().Background(cHiBg)
	sCard       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFaint).Padding(0, 2)
	sSectionHdr = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
)

// renderTabs draws a tab strip like Claude Code's /usage screen.
func renderTabs(names []string, active int) string {
	parts := make([]string, len(names))
	for i, n := range names {
		if i == active {
			parts[i] = sTabActive.Render(n)
		} else {
			parts[i] = sTabIdle.Render(n)
		}
	}
	return strings.Join(parts, " ")
}

func pctStyle(p int) lipgloss.Style {
	switch {
	case p < 0:
		return sDim
	case p >= 80:
		return sGood
	case p >= 50:
		return sWarn
	default:
		return sBad
	}
}

func pctCell(p int) string {
	if p < 0 {
		return sDim.Render("—")
	}
	return pctStyle(p).Render(fmt.Sprintf("%d%%", p))
}

var partials = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// bar draws a horizontal bar filled to frac (0..1).
func bar(frac float64, width int, style lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	frac = math.Max(0, math.Min(1, frac))
	eighths := int(math.Round(frac * float64(width*8)))
	full, rest := eighths/8, eighths%8
	s := strings.Repeat("█", full)
	if rest > 0 {
		s += partials[rest]
	}
	pad := width - full
	if rest > 0 {
		pad--
	}
	return style.Render(s) + lipgloss.NewStyle().Foreground(cFaint).Render(strings.Repeat("░", max(0, pad)))
}

func pctBar(p, width int) string {
	if p < 0 {
		return strings.Repeat(" ", width)
	}
	return bar(float64(p)/100, width, pctStyle(p))
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("02/01 15:04")
}

func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dmin", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02d", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func keyHint(k, desc string) string { return sKey.Render(k) + " " + sDim.Render(desc) }
