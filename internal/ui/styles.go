package ui

import (
	"hash/fnv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette holds the Rosé Pine colour roles (https://rosepinetheme.com/palette).
type Palette struct {
	Base, Surface, Overlay, Muted, Subtle, Text lipgloss.Color
	Love, Gold, Rose, Pine, Foam, Iris, Leaf    lipgloss.Color
	HighlightLow, HighlightMed, HighlightHigh   lipgloss.Color
	senders                                     []lipgloss.Color
}

// Official Rosé Pine variants, values from rose-pine/palette and rose-pine/neovim.
var (
	RosePine = Palette{
		Base: "#191724", Surface: "#1f1d2e", Overlay: "#26233a",
		Muted: "#6e6a86", Subtle: "#908caa", Text: "#e0def4",
		Love: "#eb6f92", Gold: "#f6c177", Rose: "#ebbcba",
		Pine: "#31748f", Foam: "#9ccfd8", Iris: "#c4a7e7", Leaf: "#95b1ac",
		HighlightLow: "#21202e", HighlightMed: "#403d52", HighlightHigh: "#524f67",
	}
	RosePineMoon = Palette{
		Base: "#232136", Surface: "#2a273f", Overlay: "#393552",
		Muted: "#6e6a86", Subtle: "#908caa", Text: "#e0def4",
		Love: "#eb6f92", Gold: "#f6c177", Rose: "#ea9a97",
		Pine: "#3e8fb0", Foam: "#9ccfd8", Iris: "#c4a7e7", Leaf: "#95b1ac",
		HighlightLow: "#2a283e", HighlightMed: "#44415a", HighlightHigh: "#56526e",
	}
	RosePineDawn = Palette{
		Base: "#faf4ed", Surface: "#fffaf3", Overlay: "#f2e9e1",
		Muted: "#9893a5", Subtle: "#797593", Text: "#464261",
		Love: "#b4637a", Gold: "#ea9d34", Rose: "#d7827e",
		Pine: "#286983", Foam: "#56949f", Iris: "#907aa9", Leaf: "#6d8f89",
		HighlightLow: "#f4ede8", HighlightMed: "#dfdad9", HighlightHigh: "#cecacd",
	}
)

func init() {
	// Group sender colours, chosen for contrast on the Overlay bubble.
	for _, p := range []*Palette{&RosePine, &RosePineMoon} {
		p.senders = []lipgloss.Color{p.Love, p.Gold, p.Rose, p.Iris, p.Leaf}
	}
	// Dawn's gold is too light on its cream background; pine stays for contrast.
	RosePineDawn.senders = []lipgloss.Color{RosePineDawn.Love, RosePineDawn.Iris, RosePineDawn.Rose,
		RosePineDawn.Leaf, RosePineDawn.Pine}
	ApplyTheme("rose-pine")
}

// ThemeByName returns the palette for a config value; unknown names fall back
// to the main variant.
func ThemeByName(name string) Palette {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "rose-pine-moon", "moon":
		return RosePineMoon
	case "rose-pine-dawn", "dawn":
		return RosePineDawn
	default:
		return RosePine
	}
}

// Active palette and derived styles. Set by ApplyTheme before the UI starts.
var (
	pal Palette

	colorBorder, colorBorderFocus, colorSelBg lipgloss.Color
	colorMeBg, colorThemBg, colorBadgeFg      lipgloss.Color
	colorBarBg                                lipgloss.Color
	colorWarm                                 lipgloss.Color // highlight: unread, insert, delivered

	styleBase, styleTitle, styleDim, styleMuted lipgloss.Style
	styleName, styleNameBold, styleBadge        lipgloss.Style
	styleErr, styleAccent, styleOnline          lipgloss.Style
	styleUnread                                 lipgloss.Style
	styleFilter, styleDate                      lipgloss.Style
	styleBubbleMe, styleBubbleThem              lipgloss.Style
	styleStampMe, styleStampThem                lipgloss.Style
	styleModeNormal, styleModeInsert            lipgloss.Style
	styleModeCmd, styleModeSearch               lipgloss.Style
	styleModeVisual                             lipgloss.Style
	styleStatusBar, styleSelected               lipgloss.Style
	styleMentionBadge, styleMention             lipgloss.Style
)

// ApplyTheme sets the active palette by config name and rebuilds all styles.
func ApplyTheme(name string) {
	pal = ThemeByName(name)
	p := pal

	colorBorder = p.HighlightMed
	colorBorderFocus = p.Rose
	colorSelBg = p.HighlightMed
	colorMeBg = p.HighlightMed
	colorThemBg = p.Overlay
	colorBadgeFg = p.Base
	colorBarBg = p.Surface
	// Gold is the highlight colour; on Dawn's cream background it's too pale
	// (2.1:1), so Dawn uses love instead.
	colorWarm = p.Gold
	if p.Base == RosePineDawn.Base {
		colorWarm = p.Love
	}

	fg := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	mode := func(c lipgloss.Color) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(p.Base).Background(c).Padding(0, 1)
	}

	styleBase = fg(p.Text)
	styleTitle = fg(p.Text).Bold(true)
	styleDim = fg(p.Subtle)
	styleMuted = fg(p.Muted)
	styleName = fg(p.Text)
	styleNameBold = fg(p.Text).Bold(true)
	styleBadge = mode(colorWarm)
	styleErr = fg(p.Love)
	styleAccent = fg(p.Rose)
	styleOnline = fg(p.Leaf)
	styleUnread = fg(colorWarm)
	styleFilter = fg(p.Gold)
	styleDate = fg(p.Subtle).Background(p.Overlay)

	styleBubbleMe = lipgloss.NewStyle().Background(colorMeBg).Foreground(p.Text).Padding(0, 1)
	styleBubbleThem = lipgloss.NewStyle().Background(colorThemBg).Foreground(p.Text).Padding(0, 1)
	styleStampMe = fg(p.Subtle)
	styleStampThem = fg(p.Subtle)

	// Mode colours follow rose-pine's lualine theme.
	styleModeNormal = mode(p.Rose)
	styleModeInsert = mode(colorWarm)
	styleModeCmd = mode(p.Love)
	styleModeSearch = mode(p.Leaf)
	styleModeVisual = mode(p.Iris)
	styleStatusBar = fg(p.Subtle).Background(colorBarBg)
	styleSelected = lipgloss.NewStyle().Background(colorSelBg)
	// messages that mention you: love, distinct from gold unread and rose selection
	styleMentionBadge = mode(p.Love)
	styleMention = fg(p.Love).Bold(true)
}

func senderColor(id string) lipgloss.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return pal.senders[h.Sum32()%uint32(len(pal.senders))]
}

func senderStyle(id string) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(senderColor(id))
}
