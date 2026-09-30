package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDroppedFiles(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.png", []byte("x"))
	spaced := writeFile(t, dir, "My Photo (1).jpg", []byte("x"))
	pdf := writeFile(t, dir, "report.pdf", []byte("x"))
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	uri := "file://" + strings.ReplaceAll(spaced, " ", "%20")
	tests := []struct {
		name, paste string
		want        []string // nil: not a drop
	}{
		{"one path", a, []string{a}},
		{"kitty: one per line", a + "\n" + spaced + "\n", []string{a, spaced}},
		{"uri-list with CRLF", "file://" + a + "\r\n" + uri + "\r\n", []string{a, spaced}},
		{"localhost uri", "file://localhost" + pdf, []string{pdf}},
		{"quoted, one line", "'" + spaced + "' '" + pdf + "'", []string{spaced, pdf}},
		{"escaped spaces", strings.ReplaceAll(spaced, " ", `\ `), []string{spaced}},
		{"space in name, unquoted", spaced, []string{spaced}},
		{"plain text", "hello there", nil},
		{"text mentioning a file", "look at " + a, nil},
		{"one missing file", a + "\n" + filepath.Join(dir, "gone.png"), nil},
		{"a folder", filepath.Join(dir, "folder"), nil},
		{"relative path", "a.png", nil},
		{"other host", "file://example.com" + a, nil},
		{"a link", "https://example.com/cat.png", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := droppedFiles(tt.paste)
			if ok != (tt.want != nil) || strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("droppedFiles(%q) = %q, %v; want %q", tt.paste, got, ok, tt.want)
			}
		})
	}
}

func dropMsg(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}
}

func TestDropFilesOnChat(t *testing.T) {
	dir := t.TempDir()
	img := writeFile(t, dir, "sunset.png", screenshot(t))
	pdf := writeFile(t, dir, "report.pdf", []byte("%PDF-1.4 fake"))
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	m, _ = press(t, m, tea.KeyEsc) // normal mode: drops work there too

	next, cmd := m.Update(dropMsg(img + "\n" + pdf))
	m = drain(t, next.(Model), cmd)
	if len(m.attachments) != 2 || !m.attachments[0].image || m.attachments[1].image {
		t.Fatalf("attachments = %+v", m.attachments)
	}
	if m.mode != modeInsert || m.compose.Value() != "" {
		t.Fatalf("mode %d, compose %q (paths shouldn't be typed)", m.mode, m.compose.Value())
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "2 to send") {
		t.Fatalf("not shown:\n%s", v)
	}

	// while typing, pasting text still types it
	next, _ = m.Update(dropMsg("hello " + pdf))
	m = next.(Model)
	if m.compose.Value() != "hello "+pdf || len(m.attachments) != 2 {
		t.Fatalf("text paste: compose %q, %d attachments", m.compose.Value(), len(m.attachments))
	}
}

func TestDropOnChatList(t *testing.T) {
	dir := t.TempDir()
	img := writeFile(t, dir, "sunset.png", []byte("x"))
	m, _ := testModel(t)
	next, cmd := m.Update(dropMsg(img))
	m = next.(Model)
	if cmd != nil || len(m.attachments) != 0 || !strings.Contains(m.notice, "open a chat first") {
		t.Fatalf("notice %q, %d attachments", m.notice, len(m.attachments))
	}
}
