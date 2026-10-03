package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var bgSeq = regexp.MustCompile(`48;2;(\d+);(\d+);(\d+)`)

// Every background the app actually draws must be listed exactly: kitty
// only makes a cell see-through when its colour matches. (It didn't:
// #403d52 is drawn as 64;60;81, so the selection stayed solid.)
func TestKittyTransparencyCoversDrawnColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	for _, theme := range []string{"rose-pine", "rose-pine-moon", "rose-pine-dawn"} {
		ApplyTheme(theme)
		on, _ := KittyTransparency(theme, 0.6)
		drawn := map[string]string{
			"selection":    styleSelected.Render("x"),
			"your bubble":  styleBubbleMe.Render("x"),
			"their bubble": styleBubbleThem.Render("x"),
			"date label":   styleDate.Render("x"),
			"status bar":   styleStatusBar.Render("x"),
			"select-all":   lipgloss.NewStyle().Background(colorSelBg).Render("x"),
		}
		for what, out := range drawn {
			m := bgSeq.FindStringSubmatch(out)
			if m == nil {
				t.Fatalf("%s/%s: no background in %q", theme, what, out)
			}
			var r, g, b int
			fmt.Sscan(m[1], &r)
			fmt.Sscan(m[2], &g)
			fmt.Sscan(m[3], &b)
			hex := fmt.Sprintf("#%02x%02x%02x@0.60", r, g, b)
			if !strings.Contains(on, hex) {
				t.Errorf("%s: %s is drawn as %s, which isn't listed in %q", theme, what, hex, on)
			}
		}
	}
	ApplyTheme("rose-pine")
}

func TestKittyTransparencyCodes(t *testing.T) {
	on, off := KittyTransparency("rose-pine", 0.6)
	if !strings.HasPrefix(on, "\x1b]21;") || !strings.HasSuffix(on, "\x1b\\") {
		t.Fatalf("on = %q", on)
	}
	if n := strings.Count(on, "transparent_background_color"); n < 3 || n > 7 {
		t.Fatalf("%d colours listed (kitty has 7 slots): %q", n, on)
	}
	// off resets every key it set
	for i := 1; i <= strings.Count(on, "="); i++ {
		if !strings.Contains(off, fmt.Sprintf("transparent_background_color%d", i)) {
			t.Fatalf("off misses slot %d: %q", i, off)
		}
	}
	if strings.Contains(off, "=") {
		t.Fatalf("off should reset, not set: %q", off)
	}
	for _, o := range []float64{1, 1.5, 0, -1} {
		if on, off := KittyTransparency("rose-pine", o); on != "" || off != "" {
			t.Errorf("opacity %v: %q %q", o, on, off)
		}
	}
}
