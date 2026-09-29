package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeStickers struct {
	mu       sync.Mutex
	stickers []messages.Message
	gifs     []messages.Message
	sent     []string // "existing:id", "sticker:path", "gif:path"
}

func (f *fakeStickers) RecentMedia(_ context.Context, typ string, _ int) ([]messages.Message, error) {
	if typ == messages.MediaGIF {
		return f.gifs, nil
	}
	return f.stickers, nil
}
func (f *fakeStickers) record(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, s)
	return nil
}
func (f *fakeStickers) SendExisting(_ context.Context, _, id string) error {
	return f.record("existing:" + id)
}
func (f *fakeStickers) SendNewSticker(_ context.Context, _, p string) error {
	return f.record("sticker:" + p)
}
func (f *fakeStickers) SendNewGIF(_ context.Context, _, p, _ string) error {
	return f.record("gif:" + p)
}

func stickerMsgs(n int, typ string) []messages.Message {
	var out []messages.Message
	for i := 0; i < n; i++ {
		var pm *waE2E.Message
		if typ == messages.MediaGIF {
			pm = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{GifPlayback: proto.Bool(true), Width: proto.Uint32(320), Height: proto.Uint32(240)}}
		} else {
			pm = &waE2E.Message{StickerMessage: &waE2E.StickerMessage{Width: proto.Uint32(512), Height: proto.Uint32(512)}}
		}
		t, blob := messagesExtract(pm)
		out = append(out, messages.Message{Id: fmt.Sprintf("%s%d", typ, i), ChatId: "x", MediaType: t, Media: blob})
	}
	return out
}

// messagesExtract marshals like the backend does.
func messagesExtract(pm *waE2E.Message) (string, []byte) {
	b, _ := proto.Marshal(pm)
	switch {
	case pm.GetVideoMessage() != nil:
		return messages.MediaGIF, b
	}
	return messages.MediaSticker, b
}

func stickerModel(t *testing.T, fs *fakeStickers) Model {
	t.Helper()
	m := New(make(chan messages.Command, 20), []*messages.Conversation{{JID: "c@s.whatsapp.net", Name: "C", LastMsgTime: 1}},
		Options{SidebarWidth: 38, Images: termimg.ModeOff, Stickers: fs, Clipboard: fakeClip{}})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	return m
}

func TestStickerPickerSendsRecent(t *testing.T) {
	fs := &fakeStickers{stickers: stickerMsgs(20, messages.MediaSticker), gifs: stickerMsgs(3, messages.MediaGIF)}
	m := stickerModel(t, fs)
	m, cmds := keys(t, m, "s")
	m = drain(t, m, tea.Batch(cmds...))
	if m.stk == nil || m.stk.loading || len(m.stk.items[tabStickers]) != 20 {
		t.Fatal("picker not loaded")
	}
	v := stripANSI(m.View())
	for _, want := range []string{"Stickers 20", "GIFs 3", "STICKERS", "enter send"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	perRow, _ := m.stickerGrid()
	m, _ = keys(t, m, "l", "l", "j") // third item of the second row
	if want := perRow + 2; m.stk.cursor[tabStickers] != want {
		t.Fatalf("cursor %d, want %d", m.stk.cursor[tabStickers], want)
	}
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if len(fs.sent) != 1 || fs.sent[0] != fmt.Sprintf("existing:sticker%d", perRow+2) {
		t.Fatalf("sent %v", fs.sent)
	}
	if m.stk != nil {
		t.Fatal("picker should close after sending")
	}
}

func TestStickerPickerGIFTabAndNewFromFile(t *testing.T) {
	fs := &fakeStickers{gifs: stickerMsgs(2, messages.MediaGIF)}
	m := stickerModel(t, fs)
	m, cmds := keys(t, m, "s")
	m = drain(t, m, tea.Batch(cmds...))
	if !strings.Contains(stripANSI(m.View()), "No stickers yet") {
		t.Fatal("empty sticker tab not explained")
	}
	m, _ = press(t, m, tea.KeyTab)
	if m.stk.tab != tabGIFs {
		t.Fatal("tab did not switch to GIFs")
	}
	// "n" opens yazi; its result makes GIFs from the files
	m, _ = keys(t, m, "n")
	next, cmd := m.Update(pickedFilesMsg{paths: []string{"/tmp/a.gif", "/tmp/b.mp4"}})
	m = drain(t, next.(Model), cmd)
	if strings.Join(fs.sent, ",") != "gif:/tmp/a.gif,gif:/tmp/b.mp4" || m.stk != nil || m.notice != "2 sent" {
		t.Fatalf("sent %v, stk %v, notice %q", fs.sent, m.stk != nil, m.notice)
	}
	if len(m.attachments) != 0 {
		t.Fatal("files went to the attachment tray instead")
	}
}

func TestStickerCommand(t *testing.T) {
	fs := &fakeStickers{}
	m := stickerModel(t, fs)
	m, _ = keys(t, m, ":")
	for _, r := range "sticker /tmp/cat.png" {
		m, _ = keys(t, m, string(r))
	}
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if len(fs.sent) != 1 || fs.sent[0] != "sticker:/tmp/cat.png" || m.notice != "Sticker sent" {
		t.Fatalf("sent %v notice %q", fs.sent, m.notice)
	}
	m, cmds := keys(t, m, ":", "g", "i", "f")
	m, cmd = press(t, m, tea.KeyEnter)
	m = drain(t, m, tea.Batch(append(cmds, cmd)...))
	if m.stk == nil || m.stk.tab != tabGIFs {
		t.Fatal(":gif should open the picker on the GIF tab")
	}
}
