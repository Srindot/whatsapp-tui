package ui

import (
	"strings"
	"testing"
)

// boxModel is a chat with "hello world" typed, back in the box's normal mode.
func boxModel(t *testing.T) Model {
	t.Helper()
	m := pasteModel(t, fakeClip{}, &fakeSender{}) // in insert mode
	for _, r := range "hello world" {
		m, _ = keys(t, m, string(r))
	}
	m, _ = keys(t, m, "esc")
	if m.mode != modeText {
		t.Fatalf("esc should go to the box's normal mode, got %d", m.mode)
	}
	return m
}

func TestTextModeMovesAndEdits(t *testing.T) {
	m := boxModel(t)
	// typing letters here doesn't add them
	m, _ = keys(t, m, "q", "z")
	if m.compose.Value() != "hello world" {
		t.Fatalf("normal mode typed: %q", m.compose.Value())
	}
	m, _ = keys(t, m, "0", "x") // delete the h
	if m.compose.Value() != "ello world" {
		t.Fatalf("0 x: %q", m.compose.Value())
	}
	m, _ = keys(t, m, "I", "H", "esc") // insert at the start
	if m.compose.Value() != "Hello world" {
		t.Fatalf("I: %q", m.compose.Value())
	}
	m, _ = keys(t, m, "A", "!", "esc") // append at the end
	if m.compose.Value() != "Hello world!" {
		t.Fatalf("A: %q", m.compose.Value())
	}
	m, _ = keys(t, m, "0", "w", "D") // delete from the second word
	if got := strings.TrimRight(m.compose.Value(), " "); got != "Hello" {
		t.Fatalf("0 w D: %q", m.compose.Value())
	}
	m, _ = keys(t, m, "d", "d")
	if m.compose.Value() != "" {
		t.Fatalf("dd: %q", m.compose.Value())
	}
	m, _ = keys(t, m, "i", "x", "y", "esc", "0", "a", "Q")
	if m.compose.Value() != "xQy" {
		t.Fatalf("a appends after the cursor: %q", m.compose.Value())
	}
}

func TestTextModeEnterSendsAndStays(t *testing.T) {
	m := boxModel(t)
	m, cmds := keys(t, m, "enter")
	if m.compose.Value() != "" || m.mode != modeText {
		t.Fatalf("after enter: %q mode %d", m.compose.Value(), m.mode)
	}
	run(cmds)
	m, _ = keys(t, m, "esc")
	if m.mode != modeNormal || m.compose.Focused() {
		t.Fatal("esc in the box should leave it")
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "NORMAL") {
		t.Fatal("badge")
	}
}

func TestTextModeBadgeAndHint(t *testing.T) {
	m := boxModel(t)
	v := stripANSI(m.View())
	if !strings.Contains(v, "TEXT") || !strings.Contains(v, "i insert · a append") {
		t.Fatalf("no TEXT badge / hint:\n%s", v)
	}
}
