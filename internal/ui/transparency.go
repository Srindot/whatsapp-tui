package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Terminals draw a cell's background colour solid, so over a see-through
// window the highlights, bubbles and status bar look like opaque slabs.
// kitty can draw chosen background colours with an opacity instead (its
// color control protocol, OSC 21 transparent_background_color1..7); this
// sets that up for the theme's fill colours, for this window only.

// fillColors are the theme colours used as backgrounds.
func fillColors(p Palette) []lipgloss.Color {
	return []lipgloss.Color{
		p.HighlightMed, // selection, your bubbles
		p.Overlay,      // their bubbles, date labels
		p.Surface,      // status bar
	}
}

// KittyTransparency returns the escape codes that make the theme's fill
// colours see-through at opacity (on), and that undo it (off). Both are
// empty when there's nothing to do (opacity at or above 1, or not positive).
func KittyTransparency(theme string, opacity float64) (on, off string) {
	if opacity <= 0 || opacity >= 1 {
		return "", ""
	}
	// kitty matches a cell's background exactly, and the styling library
	// sends slightly different values than the hex colours (termenv rounds
	// down: #403d52 goes out as 64;60;81). So list each colour as it's
	// actually sent, and the exact hex too.
	var colors []string
	seen := map[string]bool{}
	for _, c := range fillColors(ThemeByName(theme)) {
		for _, v := range []string{sentColor(string(c)), strings.ToLower(string(c))} {
			if v != "" && !seen[v] {
				seen[v] = true
				colors = append(colors, v)
			}
		}
	}
	var set, reset []string
	for i, c := range colors {
		if i == 7 { // kitty has seven slots
			break
		}
		key := fmt.Sprintf("transparent_background_color%d", i+1)
		set = append(set, fmt.Sprintf("%s=%s@%.2f", key, c, opacity))
		reset = append(reset, key)
	}
	return "\x1b]21;" + strings.Join(set, ";") + "\x1b\\",
		"\x1b]21;" + strings.Join(reset, ";") + "\x1b\\"
}

// sentColor is hex as the terminal receives it as a background, from
// termenv's own conversion ("" if it can't be parsed).
func sentColor(hex string) string {
	var r, g, b int
	seq := termenv.RGBColor(hex).Sequence(true) // "48;2;R;G;B"
	if _, err := fmt.Sscanf(seq, "48;2;%d;%d;%d", &r, &g, &b); err != nil {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
