package tui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/theme"
)

// ── colors ───────────────────────────────────────────────────────────────────

// Adaptive resolves a light/dark color pair once, at startup — v2 dropped
// AdaptiveColor, and the package-level styles below are built once, not per
// render. Exported so cmd/ can share it.
var Adaptive = func() func(light, dark string) color.Color {
	pick := lipgloss.LightDark(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
	return func(light, dark string) color.Color { return pick(lipgloss.Color(light), lipgloss.Color(dark)) }
}()

// ColorLime, ColorMuted, ColorOk and ColorFg are exported so the
// non-interactive print commands in cmd/ (suggest, today, review) can reuse
// the same palette instead of re-declaring their own copies. All of them
// come from the shared theme and are rebuilt by refreshStyles on a switch.
var (
	ColorLime, ColorMuted, ColorOk, ColorFg                     color.Color
	colorWarn, colorDanger, colorBorder, colorGroup, colorHover color.Color

	styleLime, styleMuted, styleOk, styleOkBold, styleWarn, styleWarnBd, styleDanger, styleFg, styleGroup, styleHover, panelStyle lipgloss.Style
)

func init() {
	refreshStyles()
	theme.OnChange(refreshStyles)
}

// refreshStyles builds every cached colour and style from the active theme (live theme switching).
// habctl's cursor row is marked by colour alone, so hover has its own hue (the theme blue).
func refreshStyles() {
	ColorLime, ColorMuted, ColorOk = theme.GreenV2, theme.MutedV2, theme.GreenV2
	colorWarn, colorDanger, colorBorder, colorGroup, colorHover = theme.AmberV2, theme.RedV2, theme.SubtleV2, theme.BlueV2, theme.BlueV2
	ColorFg = Adaptive("#1e293b", "#e2e8f0")

	styleLime = lipgloss.NewStyle().Foreground(ColorLime)
	styleMuted = lipgloss.NewStyle().Foreground(ColorMuted)
	styleOk = lipgloss.NewStyle().Foreground(ColorOk)
	styleOkBold = lipgloss.NewStyle().Foreground(ColorOk).Bold(true)
	styleWarn = lipgloss.NewStyle().Foreground(colorWarn)
	styleWarnBd = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	styleDanger = lipgloss.NewStyle().Foreground(colorDanger)
	styleFg = lipgloss.NewStyle().Foreground(ColorFg)
	styleGroup = lipgloss.NewStyle().Foreground(colorGroup).Bold(true)
	styleHover = lipgloss.NewStyle().Foreground(colorHover)
	panelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(1, 2)
}

var (
	// heatColors is the 5-level completion shading shared by the global
	// Stats heatmap and the per-habit detail heatmap, so both read as the
	// same visual language rather than two slightly different scales.
	heatColors = [5]lipgloss.Style{
		lipgloss.NewStyle().Foreground(Adaptive("#cbd5e1", "#2d3748")),
		lipgloss.NewStyle().Foreground(Adaptive("#86efac", "#276749")),
		lipgloss.NewStyle().Foreground(Adaptive("#4ade80", "#38a169")),
		lipgloss.NewStyle().Foreground(Adaptive("#22c55e", "#48bb78")),
		lipgloss.NewStyle().Foreground(Adaptive("#16a34a", "#68d391")),
	}
)

// sectionHeader renders the "habctl · <Section>" prefix shared by every
// view, so the app name is a constant anchor regardless of which of the
// ~20 screens is active.
func sectionHeader(section string) string {
	return styleLime.Bold(true).Render("habctl") + styleMuted.Render(" · "+section)
}

// ── command palette ("​:") ────────────────────────────────────────────────────
//
// Prototype for one tool before rolling out to the rest of the suite: types
// out full words instead of memorizing single-key shortcuts across habctl's
// ~20 views. Reuses the exact same key handling every shortcut already goes
// through (handleList) by synthesizing the mapped keypress, so behavior is
// guaranteed identical to typing the key directly.

type paletteCommand struct {
	name string // typed to match, e.g. "stats"
	desc string
	key  string // the existing single-key shortcut this command triggers
}

var paletteCommands = []paletteCommand{
	{"new", "Add a new habit", "n"},
	{"edit", "Edit selected habit", "e"},
	{"delete", "Delete selected habit", "d"},
	{"archive", "Archive selected habit", "a"},
	{"archived", "Open archive (restore / delete)", "A"},
	{"move", "Move habit to a group", "m"},
	{"groups", "Manage groups", "G"},
	{"goal", "Goal → 3 linked habits (AI decompose)", "g"},
	{"suggest", "AI suggestions (context-aware)", "s"},
	{"review", "AI weekly review — pattern coaching", "r"},
	{"stats", "Stats — heatmap & completion", "t"},
	{"chains", "Manage habit chains", "c"},
	{"settings", "AI provider & API keys", "S"},
	{"note", "Add note to today's check-in", "N"},
	{"window", "Toggle 7d / 30d streak window", "w"},
	{"compact", "Compact/normal toggle", "v"},
	{"theme", "Colour theme picker with live preview (saved for the whole suite)", "T"},
	{"help", "Show help", "?"},
	{"quit", "Quit habctl", "q"},
}

// matchPaletteCommands returns commands whose name contains q (case
// insensitive), name-prefix matches first.
func matchPaletteCommands(q string) []paletteCommand {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return paletteCommands
	}
	var prefix, contains []paletteCommand
	for _, c := range paletteCommands {
		switch {
		case strings.HasPrefix(c.name, q):
			prefix = append(prefix, c)
		case strings.Contains(c.name, q):
			contains = append(contains, c)
		}
	}
	return append(prefix, contains...)
}
