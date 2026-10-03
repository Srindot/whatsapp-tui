package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func typed(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = keys(t, m, string(r))
	}
	return m
}

func TestSelectAllReplaceAndDelete(t *testing.T) {
	m := pasteModel(t, fakeClip{}, &fakeSender{}) // typing in a chat
	m = typed(t, m, "hello world")
	m, _ = press(t, m, tea.KeyCtrlA)
	if !m.selectAll {
		t.Fatal("ctrl+a should select all")
	}
	// typing replaces everything
	m, _ = keys(t, m, "X")
	if m.compose.Value() != "X" || m.selectAll {
		t.Fatalf("after typing: %q selected %v", m.compose.Value(), m.selectAll)
	}
	// backspace on a selection clears it
	m = typed(t, m, "yz")
	m, _ = press(t, m, tea.KeyCtrlA)
	m, _ = press(t, m, tea.KeyBackspace)
	if m.compose.Value() != "" {
		t.Fatalf("after backspace: %q", m.compose.Value())
	}
	// esc only deselects; the text stays and you're still typing
	m = typed(t, m, "keep")
	m, _ = press(t, m, tea.KeyCtrlA)
	m, _ = press(t, m, tea.KeyEsc)
	if m.selectAll || m.compose.Value() != "keep" || m.mode != modeInsert {
		t.Fatalf("esc: selected %v text %q mode %d", m.selectAll, m.compose.Value(), m.mode)
	}
	// arrows deselect and move as usual
	m, _ = press(t, m, tea.KeyCtrlA)
	m, _ = press(t, m, tea.KeyLeft)
	m, _ = keys(t, m, "!")
	if m.compose.Value() != "kee!p" {
		t.Fatalf("arrow then type: %q", m.compose.Value())
	}
}

func TestSelectAllCopyAndCut(t *testing.T) {
	var got copied
	m := pasteModel(t, fakeClip{wrote: &got}, &fakeSender{})
	m = typed(t, m, "copy me")
	m, _ = press(t, m, tea.KeyCtrlA)
	m, cmd := press(t, m, tea.KeyCtrlC) // copies; doesn't quit
	if cmd == nil {
		t.Fatal("ctrl+c did nothing")
	}
	if msg := cmd(); msg == tea.Quit() {
		t.Fatal("ctrl+c quit with text selected")
	}
	if got.text != "copy me" || m.compose.Value() != "copy me" {
		t.Fatalf("copied %q, text now %q", got.text, m.compose.Value())
	}
	m, _ = press(t, m, tea.KeyCtrlA)
	m, cmd = press(t, m, tea.KeyCtrlX) // cut
	cmd()
	if got.text != "copy me" || m.compose.Value() != "" {
		t.Fatalf("cut: copied %q, text now %q", got.text, m.compose.Value())
	}
}

func TestSelectAllShowsHighlight(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	m = typed(t, m, "hi there")
	m, _ = press(t, m, tea.KeyCtrlA)
	if v := m.renderCompose(60); !strings.Contains(stripANSI(v), "hi there") || v == stripANSI(v) {
		t.Fatalf("selection not drawn: %q", v)
	}
}

func TestWordKeys(t *testing.T) {
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	m = typed(t, m, "one two three")
	m, _ = press(t, m, tea.KeyCtrlH) // ctrl+backspace: delete a word
	if got := m.compose.Value(); got != "one two " {
		t.Fatalf("delete word: %q", got)
	}
	m, _ = press(t, m, tea.KeyCtrlLeft)
	m, _ = keys(t, m, "X")
	if got := m.compose.Value(); got != "one Xtwo " {
		t.Fatalf("ctrl+left then type: %q", got)
	}
}

func TestCtrlANoLongerAttaches(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	m, cmd := press(t, m, tea.KeyCtrlA)
	if cmd != nil || strings.Contains(m.notice, "yazi") {
		t.Fatalf("ctrl+a still tries to attach (notice %q)", m.notice)
	}
}
