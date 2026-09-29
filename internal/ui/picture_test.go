package ui

import (
	"context"
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakePictures struct{ paths map[string]string }

func (f fakePictures) FullPicture(_ context.Context, jid string) (string, error) {
	return f.paths[jid], nil
}

func pictureModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	pic := filepath.Join(dir, "p.png")
	writePNG(t, pic, fill(640, 640, color.RGBA{235, 111, 146, 255}))
	chats := []*messages.Conversation{
		{JID: "a@s.whatsapp.net", Name: "Aneesh", LastMsgTime: 3},
		{JID: "b@s.whatsapp.net", Name: "No Pic", LastMsgTime: 2},
	}
	m := New(make(chan messages.Command, 10), chats, Options{SidebarWidth: 38, Images: termimg.ModeBlocks,
		Pictures: fakePictures{paths: map[string]string{"a@s.whatsapp.net": pic}}})
	m.img.cellW, m.img.cellH = 8, 16
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return next.(Model)
}

func TestPictureFullScreen(t *testing.T) {
	m := pictureModel(t)
	m, cmds := keys(t, m, "V")
	m = drain(t, m, tea.Batch(cmds...))
	if m.pic == nil || m.pic.loading || m.pic.image == "" {
		t.Fatalf("picture not loaded: %+v", m.pic)
	}
	v := m.View()
	if !strings.Contains(v, "38;2;235;111;146") {
		t.Fatal("picture not drawn")
	}
	plain := stripANSI(v)
	if !strings.Contains(plain, "Aneesh") || !strings.Contains(plain, "640×640") || !strings.Contains(plain, "o open in viewer") {
		t.Fatalf("caption missing:\n%s", plain)
	}
	// big: most of the screen height
	if n := strings.Count(v, "▀"); n < 30*30 {
		t.Fatalf("picture too small (%d cells)", n)
	}
	if lines := strings.Count(v, "\n") + 1; lines != 40 {
		t.Fatalf("view has %d lines", lines)
	}
	m, _ = keys(t, m, "esc")
	if m.pic != nil {
		t.Fatal("esc did not close")
	}
}

func TestPictureNone(t *testing.T) {
	m := pictureModel(t)
	m, cmds := keys(t, m, "j", "V")
	m = drain(t, m, tea.Batch(cmds...))
	if v := stripANSI(m.View()); !strings.Contains(v, "No profile picture") || !strings.Contains(v, "No Pic") {
		t.Fatalf("no-picture view:\n%s", v)
	}
	m, _ = keys(t, m, "V")
	if m.pic != nil {
		t.Fatal("V should close it again")
	}
}
