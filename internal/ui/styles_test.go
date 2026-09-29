package ui

import (
	"strings"
	"testing"
)

func TestThemeByName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"rose-pine", "#191724"},
		{"", "#191724"},
		{"unknown", "#191724"},
		{"rose-pine-moon", "#232136"},
		{"Moon", "#232136"},
		{"rose-pine-dawn", "#faf4ed"},
		{" dawn ", "#faf4ed"},
	}
	for _, tt := range tests {
		if got := string(ThemeByName(tt.name).Base); got != tt.want {
			t.Errorf("ThemeByName(%q).Base = %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestPaintBackground(t *testing.T) {
	const bg = "\x1b[48;2;25;23;36m"
	tests := []struct {
		name, in, want string
	}{
		{"disabled", "a\x1b[0mb", "a\x1b[0mb"},
		{"plain lines", "a\nb", bg + "a\x1b[0m\n" + bg + "b\x1b[0m"},
		{"after full reset", "\x1b[1mx\x1b[0my", bg + "\x1b[1mx\x1b[0m" + bg + "y\x1b[0m"},
		{"after short reset", "x\x1b[my", bg + "x\x1b[m" + bg + "y\x1b[0m"},
		{"after default bg", "x\x1b[49my", bg + "x\x1b[49m" + bg + "y\x1b[0m"},
		{"other SGR untouched", "\x1b[38;2;1;2;3mx", bg + "\x1b[38;2;1;2;3mx\x1b[0m"},
	}
	for _, tt := range tests {
		b := bg
		if tt.name == "disabled" {
			b = ""
		}
		if got := paintBackground(tt.in, b); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSenderColorStable(t *testing.T) {
	ApplyTheme("rose-pine")
	a, b := senderColor("123@s.whatsapp.net"), senderColor("123@s.whatsapp.net")
	if a != b || !strings.HasPrefix(string(a), "#") {
		t.Fatalf("sender colour not stable: %s %s", a, b)
	}
}
