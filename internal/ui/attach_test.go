package ui

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/clipboard"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeClip struct {
	img  []byte
	text string
	// what was copied
	wrote *copied
}

type copied struct {
	text, mime string
	image      []byte
}

func (c fakeClip) WriteText(text string) error {
	if c.wrote != nil {
		c.wrote.text = text
	}
	return nil
}

func (c fakeClip) WriteImage(data []byte, mime, text string) error {
	if c.wrote != nil {
		c.wrote.image, c.wrote.mime, c.wrote.text = data, mime, text
	}
	return nil
}

func (c fakeClip) Image() ([]byte, string, error) {
	if c.img == nil {
		return nil, "", clipboard.ErrEmpty
	}
	return c.img, "image/png", nil
}

func (c fakeClip) Text() (string, error) {
	if c.text == "" {
		return "", clipboard.ErrEmpty
	}
	return c.text, nil
}

type fakeSender struct {
	mu      sync.Mutex
	chat    string
	data    []byte
	caption string
	err     error
	images  int      // SendImage calls
	docs    []string // "path|caption"
}

func (s *fakeSender) SendDocument(_ context.Context, chat, path, caption string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs = append(s.docs, path+"|"+caption)
	return s.err
}

func (s *fakeSender) SendImage(_ context.Context, chat string, data []byte, caption string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chat, s.data, s.caption = chat, data, caption
	s.images++
	return s.err
}

func screenshot(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, fill(640, 360, color.RGBA{30, 30, 46, 255})); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pasteModel(t *testing.T, clip Clipboard, s Sender) Model {
	t.Helper()
	m := New(make(chan messages.Command, 10), []*messages.Conversation{{JID: "333@s.whatsapp.net", Name: "Bob", LastMsgTime: 200}},
		Options{SidebarWidth: 30, Images: termimg.ModeBlocks, Sender: s, Clipboard: clip})
	m.img.cellW, m.img.cellH = 8, 16
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter", "i")
	return m
}

func press(t *testing.T, m Model, k tea.KeyType) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: k})
	return next.(Model), cmd
}

func TestPasteImageAndSend(t *testing.T) {
	shot := screenshot(t)
	s := &fakeSender{}
	m := pasteModel(t, fakeClip{img: shot, text: "ignored"}, s)
	vpBefore := m.vp.Height

	m, cmd := press(t, m, tea.KeyCtrlV)
	m = drain(t, m, cmd)
	if len(m.attachments) != 1 || m.attachments[0].width != 640 || m.attachments[0].height != 360 {
		t.Fatalf("attachments = %+v", m.attachments)
	}
	if m.vp.Height != vpBefore-attachPreviewRows {
		t.Fatalf("viewport not shrunk for the preview: %d -> %d", vpBefore, m.vp.Height)
	}
	v := stripANSI(m.View())
	if !strings.Contains(v, "Pasted image") || !strings.Contains(v, "640×360") || !strings.Contains(v, "add a caption") {
		t.Fatalf("attachment not shown:\n%s", v)
	}

	m, _ = keys(t, m, "l", "o", "o", "k")
	m, cmd = press(t, m, tea.KeyEnter)
	if len(m.attachments) != 0 || m.compose.Value() != "" {
		t.Fatal("attachment/caption not cleared after send")
	}
	m = drain(t, m, cmd)
	if s.chat != "333@s.whatsapp.net" || s.caption != "look" || !bytes.Equal(s.data, shot) {
		t.Fatalf("sent chat=%q caption=%q %d bytes", s.chat, s.caption, len(s.data))
	}
	if m.notice != "Sent" || m.noticeErr {
		t.Fatalf("notice = %q", m.notice)
	}
	if m.vp.Height != vpBefore {
		t.Fatal("viewport not restored")
	}
}

func TestPasteRemoveAndErrors(t *testing.T) {
	s := &fakeSender{err: errors.New("upload failed")}
	m := pasteModel(t, fakeClip{img: screenshot(t)}, s)
	m, cmd := press(t, m, tea.KeyCtrlV)
	m = drain(t, m, cmd)
	m, _ = press(t, m, tea.KeyCtrlX)
	if len(m.attachments) != 0 {
		t.Fatal("ctrl+x did not remove the attachment")
	}

	m, cmd = press(t, m, tea.KeyCtrlV)
	m = drain(t, m, cmd)
	m, cmd = press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if !m.noticeErr || !strings.Contains(m.notice, "upload failed") {
		t.Fatalf("send error not shown: %q", m.notice)
	}
}

func TestPasteTextInsertsAtCursor(t *testing.T) {
	m := pasteModel(t, fakeClip{text: "big\nworld"}, &fakeSender{})
	m, _ = keys(t, m, "h", "i", " ", "!")
	m, _ = press(t, m, tea.KeyLeft)
	m, cmd := press(t, m, tea.KeyCtrlV)
	m = drain(t, m, cmd)
	if got := m.compose.Value(); got != "hi big\nworld!" { // multi-line input keeps newlines
		t.Fatalf("compose = %q", got)
	}
	if len(m.attachments) != 0 {
		t.Fatal("text paste created an attachment")
	}
}

func TestPasteEmptyClipboard(t *testing.T) {
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	m, cmd := press(t, m, tea.KeyCtrlV)
	m = drain(t, m, cmd)
	if !m.noticeErr || !strings.Contains(m.notice, "clipboard is empty") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestCtrlVFromNormalModeStartsInsert(t *testing.T) {
	m := pasteModel(t, fakeClip{img: screenshot(t)}, &fakeSender{})
	m, _ = press(t, m, tea.KeyEsc)
	m, cmd := press(t, m, tea.KeyCtrlV)
	m = drain(t, m, cmd)
	if m.mode != modeInsert || len(m.attachments) != 1 {
		t.Fatalf("mode=%d attachments=%d", m.mode, len(m.attachments))
	}
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAttachFilesAndSend(t *testing.T) {
	dir := t.TempDir()
	img := writeFile(t, dir, "sunset.png", screenshot(t))
	pdf := writeFile(t, dir, "report.pdf", []byte("%PDF-1.4 fake"))
	s := &fakeSender{}
	m := pasteModel(t, fakeClip{}, s)
	m, _ = press(t, m, tea.KeyEsc)

	// :attach with two paths (what yazi returns, too)
	next, cmd := m.Update(pickedFilesMsg{paths: []string{img, pdf}})
	m = drain(t, next.(Model), cmd)
	if len(m.attachments) != 2 || !m.attachments[0].image || m.attachments[1].image {
		t.Fatalf("attachments = %+v", m.attachments)
	}
	if m.mode != modeInsert || m.lastPickDir != dir {
		t.Fatalf("mode %d, lastPickDir %q", m.mode, m.lastPickDir)
	}
	v := stripANSI(m.View())
	for _, want := range []string{"2 to send", "sunset.png", "report.pdf", "document", "caption for the first"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}

	m, _ = keys(t, m, "l", "o", "o", "k")
	m, cmd = press(t, m, tea.KeyEnter)
	if len(m.attachments) != 0 || m.notice != "Sending 2 files…" {
		t.Fatalf("after enter: %d attachments, notice %q", len(m.attachments), m.notice)
	}
	m = drain(t, m, cmd)
	if s.images != 1 || s.caption != "look" {
		t.Fatalf("image sends %d, caption %q", s.images, s.caption)
	}
	if len(s.docs) != 1 || s.docs[0] != pdf+"|" {
		t.Fatalf("docs = %v (caption belongs to the first only)", s.docs)
	}
	if m.notice != "Sent 2 files" {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestAttachDropLastAndErrors(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.txt", []byte("a"))
	b := writeFile(t, dir, "b.txt", []byte("b"))
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	next, cmd := m.Update(pickedFilesMsg{paths: []string{a, b, filepath.Join(dir, "missing.txt"), dir}})
	m = drain(t, next.(Model), cmd)
	if len(m.attachments) != 2 {
		t.Fatalf("%d attachments", len(m.attachments))
	}
	if !m.noticeErr || !strings.Contains(m.notice, "missing.txt") || !strings.Contains(m.notice, "folder") {
		t.Fatalf("notice = %q", m.notice)
	}
	m, _ = press(t, m, tea.KeyCtrlX)
	if len(m.attachments) != 1 || m.attachments[0].name != "a.txt" {
		t.Fatal("ctrl+x should drop the last attachment")
	}
	// cancelling yazi (nothing chosen) changes nothing
	next, cmd = m.Update(pickedFilesMsg{})
	m = drain(t, next.(Model), cmd)
	if len(m.attachments) != 1 {
		t.Fatal("empty pick changed attachments")
	}
}

func TestPickFilesWithoutYazi(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	m, cmd := press(t, m, tea.KeyCtrlA)
	m = drain(t, m, cmd)
	if !m.noticeErr || !strings.Contains(m.notice, "yazi not found") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestAttachCommandWithSpacesInPath(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "my notes.txt", []byte("x"))
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	m, _ = press(t, m, tea.KeyEsc)
	m, _ = keys(t, m, ":")
	for _, r := range "attach " + p {
		m, _ = keys(t, m, string(r))
	}
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if len(m.attachments) != 1 || m.attachments[0].name != "my notes.txt" {
		t.Fatalf("attachments = %+v notice %q", m.attachments, m.notice)
	}
}

func TestAKeyAttachesInsteadOfInsert(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no yazi: the attach attempt reports it
	m := pasteModel(t, fakeClip{}, &fakeSender{})
	m, _ = press(t, m, tea.KeyEsc)
	m, cmds := keys(t, m, "a")
	m = drain(t, m, tea.Batch(cmds...))
	if m.mode == modeInsert {
		t.Fatal("a should attach, not start insert mode")
	}
	if !strings.Contains(m.notice, "yazi not found") {
		t.Fatalf("a did not try to attach: notice %q", m.notice)
	}
	m, _ = keys(t, m, "A")
	if m.mode != modeNormal || m.notice != "yazi not found; install it or use :attach <path>" {
		t.Fatalf("A should do nothing in a chat now (mode %d)", m.mode)
	}
	m, _ = keys(t, m, "i")
	if m.mode != modeInsert {
		t.Fatal("i should still start insert mode")
	}
}
