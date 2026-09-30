package ui

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/skratchdot/open-golang/open"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// viewActions serves media files for the viewer.
type viewActions struct {
	fakeActions
	path    string
	failDL  bool
	savedTo []string
}

func (a *viewActions) MediaPath(context.Context, string) (string, error) {
	if a.failDL {
		return "", errors.New("media expired")
	}
	return a.path, nil
}

func (a *viewActions) SaveMedia(_ context.Context, id, dir string) (string, error) {
	a.savedTo = append(a.savedTo, dir)
	return a.path, nil
}

func mediaMsg(t *testing.T, id string, pm *waE2E.Message, text string) messages.Message {
	t.Helper()
	b, err := proto.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	typ := messages.MediaImage
	switch {
	case pm.GetVideoMessage() != nil && pm.GetVideoMessage().GetGifPlayback():
		typ = messages.MediaGIF
	case pm.GetVideoMessage() != nil:
		typ = messages.MediaVideo
	case pm.GetStickerMessage() != nil:
		typ = messages.MediaSticker
	}
	return messages.Message{Id: id, ChatId: groupJID, ContactId: "91111@s.whatsapp.net", ContactShort: "Arjun",
		Timestamp: 1700000000, Text: text, MediaType: typ, Media: b}
}

func viewerModel(t *testing.T, a *viewActions, msgs ...messages.Message) Model {
	t.Helper()
	m := New(make(chan messages.Command, 10), []*messages.Conversation{{JID: groupJID, Name: "Hostel", LastMsgTime: 1}},
		Options{SidebarWidth: 38, Images: termimg.ModeBlocks, Actions: a})
	m.img.cellW, m.img.cellH = 8, 16
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	next, _ = m.Update(screenMsg(msgs))
	return next.(Model)
}

func TestViewPhotoFullScreen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writePNG(t, path, fill(1200, 800, color.RGBA{156, 207, 216, 255}))
	a := &viewActions{path: path}
	photo := mediaMsg(t, "p1", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Width: proto.Uint32(1200), Height: proto.Uint32(800)}}, "[IMAGE] sunset at the lake")
	m := viewerModel(t, a, photo)

	m, cmds := keys(t, m, "v", " ")
	m = drain(t, m, tea.Batch(cmds...))
	if m.view == nil || m.view.loading || m.view.err != nil {
		t.Fatalf("viewer: %+v", m.view)
	}
	v := m.View()
	if n := strings.Count(v, "▀"); n < 60*25 {
		t.Fatalf("photo too small full screen (%d cells)", n)
	}
	plain := stripANSI(v)
	for _, want := range []string{"1200×800", "sunset at the lake", "Arjun", "o open in its app", "VIEW"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q:\n%s", want, plain)
		}
	}
	if lines := strings.Count(v, "\n") + 1; lines != 40 {
		t.Fatalf("viewer overflows: %d lines", lines)
	}
	m, _ = keys(t, m, " ")
	if m.view != nil || m.mode != modeVisual {
		t.Fatal("space should close the viewer and stay in visual mode")
	}
}

func TestViewFallsBackToPreview(t *testing.T) {
	var thumb []byte
	{
		dir := t.TempDir()
		p := filepath.Join(dir, "t.png")
		writePNG(t, p, fill(40, 30, color.RGBA{235, 111, 146, 255}))
		thumb = mustRead(t, p)
	}
	a := &viewActions{failDL: true}
	photo := mediaMsg(t, "p2", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Width: proto.Uint32(400), Height: proto.Uint32(300), JPEGThumbnail: thumb}}, "[IMAGE]")
	m := viewerModel(t, a, photo)
	m, cmds := keys(t, m, "v", " ")
	m = drain(t, m, tea.Batch(cmds...))
	if m.view == nil || m.view.err != nil || !strings.Contains(stripANSI(m.View()), "small preview") {
		t.Fatalf("preview fallback: %+v", m.view)
	}
}

func TestViewNothingToView(t *testing.T) {
	m := viewerModel(t, &viewActions{}, messages.Message{Id: "t", ChatId: groupJID, ContactId: "x", Timestamp: 1, Text: "just text"})
	m, _ = keys(t, m, "v", " ")
	if m.view != nil || !m.noticeErr || !strings.Contains(m.notice, "nothing to view") {
		t.Fatalf("view=%v notice=%q", m.view != nil, m.notice)
	}
}

func TestSpaceOnVideoDownloadsAndPlays(t *testing.T) {
	var opened []string
	openURL = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURL = open.Start }()
	t.Setenv("PATH", t.TempDir()) // no mpv: falls back to the default app

	a := &viewActions{path: "/tmp/clip.mp4"}
	video := mediaMsg(t, "v1", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(12)}}, "[VIDEO]")
	m := viewerModel(t, a, video)
	m, cmds := keys(t, m, "v", " ")
	m = drain(t, m, tea.Batch(cmds...))
	if m.view != nil {
		t.Fatal("videos play in a player, not the image viewer")
	}
	if len(a.savedTo) != 1 || len(opened) != 1 || opened[0] != "/tmp/clip.mp4" {
		t.Fatalf("saved %v, opened %v", a.savedTo, opened)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
