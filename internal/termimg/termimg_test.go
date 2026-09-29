package termimg

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestPlaceholderShape(t *testing.T) {
	p := Placeholder(0x012345, 5, 3)
	lines := strings.Split(p, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	for r, l := range lines {
		if w := lipgloss.Width(l); w != 5 {
			t.Fatalf("row %d width %d, want 5", r, w)
		}
		if !strings.HasPrefix(l, "\x1b[38;2;1;35;69m") {
			t.Fatalf("row %d: id colour missing: %q", r, l)
		}
		runes := []rune(strings.TrimSuffix(strings.TrimPrefix(l, "\x1b[38;2;1;35;69m"), "\x1b[39m"))
		if runes[0] != 0x10EEEE || runes[1] != diacritics[r] || runes[2] != diacritics[0] {
			t.Fatalf("row %d: bad first cell %U", r, runes[:3])
		}
		if n := strings.Count(string(runes), "\U0010EEEE"); n != 5 {
			t.Fatalf("row %d: %d placeholders", r, n)
		}
	}
}

func TestFitCells(t *testing.T) {
	tests := []struct {
		name                     string
		w, h, maxC, maxR, cw, ch int
		wantC, wantR             int
	}{
		{"landscape limited by width", 1600, 900, 40, 20, 8, 16, 40, 11},
		{"portrait limited by height", 900, 1600, 40, 12, 8, 16, 14, 12},
		{"square", 512, 512, 20, 10, 8, 16, 20, 10},
		{"unknown size", 0, 0, 40, 20, 8, 16, 40, 15},
		{"tiny stays >=1", 1, 1000, 40, 5, 8, 16, 1, 5},
	}
	for _, tt := range tests {
		c, r := FitCells(tt.w, tt.h, tt.maxC, tt.maxR, tt.cw, tt.ch)
		if c != tt.wantC || r != tt.wantR {
			t.Errorf("%s: got %dx%d, want %dx%d", tt.name, c, r, tt.wantC, tt.wantR)
		}
	}
}

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestBlocks(t *testing.T) {
	out := Blocks(solid(10, 10, color.NRGBA{200, 10, 20, 255}), 4, 2)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines", len(lines))
	}
	for _, l := range lines {
		if got := sgr.ReplaceAllString(l, ""); got != "▀▀▀▀" {
			t.Fatalf("cells = %q", got)
		}
		if !strings.Contains(l, "38;2;200;10;20;48;2;200;10;20m") {
			t.Fatalf("colour missing: %q", l)
		}
	}
	// Transparent pixels keep the terminal background.
	if got := sgr.ReplaceAllString(Blocks(solid(4, 4, color.NRGBA{}), 2, 1), ""); got != "  " {
		t.Fatalf("transparent = %q", got)
	}
}

func TestCircle(t *testing.T) {
	c := Circle(solid(30, 20, color.NRGBA{1, 2, 3, 255}), 16)
	if c.Bounds().Dx() != 16 || c.Bounds().Dy() != 16 {
		t.Fatalf("size %v", c.Bounds())
	}
	if c.NRGBAAt(0, 0).A != 0 || c.NRGBAAt(8, 8).A != 255 {
		t.Fatalf("corner alpha %d, centre alpha %d", c.NRGBAAt(0, 0).A, c.NRGBAAt(8, 8).A)
	}
}

func TestKittyShow(t *testing.T) {
	var out bytes.Buffer
	k, err := NewKitty(&out)
	if err != nil {
		t.Fatal(err)
	}
	p, err := k.Show(solid(8, 8, color.NRGBA{9, 9, 9, 255}), 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^\x1b_Ga=T,t=t,f=100,U=1,q=2,i=(\d+),c=3,r=2;([A-Za-z0-9+/=]+)\x1b\\$`).FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("unexpected escape %q", out.String())
	}
	path, _ := base64.StdEncoding.DecodeString(m[2])
	if !strings.Contains(string(path), "tty-graphics-protocol") {
		t.Fatalf("temp path %q won't be cleaned up by kitty", path)
	}
	if _, err := os.Stat(string(path)); err != nil {
		t.Fatalf("png not written: %v", err)
	}
	if strings.Count(p, "\n") != 1 || lipgloss.Width(strings.Split(p, "\n")[0]) != 3 {
		t.Fatalf("placeholder shape wrong: %q", p)
	}

	out.Reset()
	k.Close()
	if !strings.Contains(out.String(), "a=d,d=I,i="+m[1]) {
		t.Fatalf("Close didn't delete image: %q", out.String())
	}
	if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
		t.Fatal("temp dir not removed")
	}
}

func TestDetect(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-256color")
	if Detect("auto") != ModeBlocks || Detect("off") != ModeOff || Detect("kitty") != ModeKitty {
		t.Fatal("explicit / non-kitty detection wrong")
	}
	t.Setenv("TERM", "xterm-kitty")
	if Detect("") != ModeKitty {
		t.Fatal("kitty not detected")
	}
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	if Detect("auto") != ModeBlocks {
		t.Fatal("tmux should fall back to blocks")
	}
}

func TestAvatarInitial(t *testing.T) {
	tests := []struct {
		name string
		want rune
		ok   bool
	}{
		{"Fam ✨", 'F', true},
		{"monish", 'M', true},
		{"~ óscar", 'Ó', true},
		{"+91∙∙∙∙44", 0, false},
		{"🙂", 0, false},
		{"😔 sad times", 'S', true},
		{"", 0, false},
	}
	for _, tt := range tests {
		r, ok := AvatarInitial(tt.name)
		if r != tt.want || ok != tt.ok {
			t.Errorf("AvatarInitial(%q) = %q,%v want %q,%v", tt.name, r, ok, tt.want, tt.ok)
		}
	}
}

func TestLetterAvatar(t *testing.T) {
	bg, fg := color.NRGBA{200, 0, 0, 255}, color.NRGBA{0, 0, 200, 255}
	for _, l := range []rune{'W', 0, '字'} { // letter, person icon, glyph missing from the font
		a := LetterAvatar(l, bg, fg, 64)
		if a.NRGBAAt(0, 0).A != 0 {
			t.Fatalf("%q: corner not transparent", l)
		}
		if a.NRGBAAt(32, 2).R < 150 {
			t.Fatalf("%q: disc colour missing at the top", l)
		}
		ink := 0
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				if c := a.NRGBAAt(x, y); c.B > 150 && c.R < 100 {
					ink++
				}
			}
		}
		if ink < 100 {
			t.Fatalf("%q: letter/icon not drawn (%d px)", l, ink)
		}
	}
}

func TestKittyShowFramesAnimates(t *testing.T) {
	var out bytes.Buffer
	k, err := NewKitty(&out)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	frames := []Frame{
		{Img: solid(4, 4, color.NRGBA{255, 0, 0, 255}), Delay: 50 * time.Millisecond},
		{Img: solid(4, 4, color.NRGBA{0, 255, 0, 255}), Delay: 70 * time.Millisecond},
		{Img: solid(4, 4, color.NRGBA{0, 0, 255, 255}), Delay: 90 * time.Millisecond},
	}
	if _, err := k.ShowFrames(frames, 2, 1); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	id := regexp.MustCompile(`a=T,[^;]*i=(\d+)`).FindStringSubmatch(s)[1]
	for _, want := range []string{
		"a=f,t=t,f=100,X=1,q=2,i=" + id + ",z=70;",
		"a=f,t=t,f=100,X=1,q=2,i=" + id + ",z=90;",
		"a=a,q=2,i=" + id + ",r=1,z=50",
		"a=a,q=2,i=" + id + ",s=3,v=1",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	// A still image must not start an animation.
	out.Reset()
	if _, err := k.Show(frames[0].Img, 2, 1); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "a=a") || strings.Contains(out.String(), "a=f") {
		t.Fatalf("still image sent animation codes: %q", out.String())
	}
}
