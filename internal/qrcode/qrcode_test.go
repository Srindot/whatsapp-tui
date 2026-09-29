package qrcode

import (
	"regexp"
	"strings"
	"testing"

	"github.com/skip2/go-qrcode"
)

var ansiRe = regexp.MustCompile("\033\\[[0-9;]*m")

func TestRenderCompact_RoundTrip(t *testing.T) {
	content := "2@" + strings.Repeat("AbCdEfGh12345678", 15) + ",key,id"
	qr, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		t.Fatal(err)
	}
	bmp := qr.Bitmap()
	out := New().SetCompact(true).Get(content)
	if out == nil {
		t.Fatal("Get returned nil")
	}

	reverse := make(map[rune]int, len(octantChars))
	for i, r := range octantChars {
		if _, dup := reverse[r]; dup {
			t.Fatalf("octantChars: %U used for more than one pattern", r)
		}
		reverse[r] = i
	}

	lines := strings.Split(strings.TrimSuffix(ansiRe.ReplaceAllString(string(*out), ""), "\n"), "\n")
	margin := 2
	size := len(bmp) - 2*margin
	if want := (size + 3) / 4; len(lines) != want {
		t.Fatalf("expected %d lines, got %d", want, len(lines))
	}

	for li, line := range lines {
		cells := []rune(line)
		if want := (size + 1) / 2; len(cells) != want {
			t.Fatalf("line %d: expected %d cells, got %d", li, want, len(cells))
		}
		for ci, ch := range cells {
			idx, ok := reverse[ch]
			if !ok {
				t.Fatalf("line %d col %d: unexpected char %q", li, ci, ch)
			}
			for bit := 0; bit < 8; bit++ {
				r, c := li*4+bit/2, ci*2+bit%2
				want := r < size && c < size && bmp[r+margin][c+margin]
				if got := idx&(1<<bit) != 0; got != want {
					t.Fatalf("module (%d,%d): got %v, want %v", r, c, got, want)
				}
			}
		}
	}
}
