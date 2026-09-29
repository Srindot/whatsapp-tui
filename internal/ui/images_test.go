package ui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeMedia struct {
	mu        sync.Mutex
	dir       string
	calls     []string
	noPic     bool
	failDL    bool
	mediaPath string // overrides dir/full.png for DownloadMedia
	fullImg   image.Image
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeMedia) DownloadMedia(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "media:"+id)
	f.mu.Unlock()
	if f.failDL {
		return "", errors.New("media expired")
	}
	if f.mediaPath != "" {
		return f.mediaPath, nil
	}
	return filepath.Join(f.dir, "full.png"), nil
}

func (f *fakeMedia) ProfilePicture(_ context.Context, jid string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "avatar:"+jid)
	f.mu.Unlock()
	if f.noPic {
		return "", nil
	}
	return filepath.Join(f.dir, "full.png"), nil
}

func fill(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func jpegBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func imageMessage(t *testing.T, id string) messages.Message {
	t.Helper()
	blob, err := proto.Marshal(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Width: proto.Uint32(400), Height: proto.Uint32(300),
		JPEGThumbnail: jpegBytes(t, fill(40, 30, color.RGBA{0, 0, 255, 255})),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return messages.Message{Id: id, ChatId: "333@s.whatsapp.net", ContactId: "333",
		Timestamp: 1700000000, Text: "[IMAGE] sunset", MediaType: messages.MediaImage, Media: blob}
}

// drain runs cmds (recursively through batches) and feeds results back.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for i := 0; i < 200 && len(queue) > 0; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case nil:
		default:
			next, more := m.Update(msg)
			m = next.(Model)
			queue = append(queue, more)
		}
	}
	return m
}

func mediaModel(t *testing.T, src *fakeMedia, mode termimg.Mode) Model {
	t.Helper()
	cmds := make(chan messages.Command, 50)
	chats := []*messages.Conversation{
		{JID: "333@s.whatsapp.net", Name: "Bob", LastMsgTime: 200},
	}
	m := New(cmds, chats, Options{SidebarWidth: 30, Images: mode, Media: src})
	m.img.cellW, m.img.cellH = 8, 16
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return next.(Model)
}

func TestImageThumbnailThenFull(t *testing.T) {
	src := &fakeMedia{dir: t.TempDir()}
	writePNG(t, filepath.Join(src.dir, "full.png"), fill(400, 300, color.RGBA{255, 0, 0, 255}))
	m := mediaModel(t, src, termimg.ModeBlocks)

	m, cmds := keys(t, m, "enter")
	next, cmd := m.Update(screenMsg{imageMessage(t, "img1")})
	m = next.(Model)

	// Before loading: the space is reserved.
	if !strings.Contains(stripANSI(m.View()), "loading…") {
		t.Fatal("no loading placeholder")
	}
	_ = cmds
	m = drain(t, m, cmd)

	e := m.img.get(msgKey(m, imageMessage(t, "img1")))
	if e == nil {
		t.Fatal("no entry for the image")
	}
	if e.state != imgReady || !e.full {
		t.Fatalf("state=%d full=%v, want ready+full", e.state, e.full)
	}
	v := m.View()
	if strings.Contains(stripANSI(v), "loading…") {
		t.Fatal("still loading after download")
	}
	if !strings.Contains(v, "38;2;255;0;0") {
		t.Fatal("full image (red) not rendered")
	}
	if !strings.Contains(stripANSI(v), "sunset") {
		t.Fatal("caption missing")
	}
}

func TestImageDownloadFailsKeepsThumbnail(t *testing.T) {
	src := &fakeMedia{dir: t.TempDir(), failDL: true}
	m := mediaModel(t, src, termimg.ModeBlocks)
	m, _ = keys(t, m, "enter")
	next, cmd := m.Update(screenMsg{imageMessage(t, "img2")})
	m = drain(t, next.(Model), cmd)
	e := m.img.get(msgKey(m, imageMessage(t, "img2")))
	if e == nil || e.state != imgReady || e.full {
		t.Fatalf("entry = %+v, want thumbnail kept", e)
	}
	if !strings.Contains(m.View(), "38;2;0;0;25") { // blue-ish thumbnail
		t.Fatal("thumbnail not rendered")
	}
}

func TestImagesOffShowsLabel(t *testing.T) {
	src := &fakeMedia{dir: t.TempDir()}
	m := mediaModel(t, src, termimg.ModeOff)
	m, _ = keys(t, m, "enter")
	next, cmd := m.Update(screenMsg{imageMessage(t, "img3")})
	m = drain(t, next.(Model), cmd)
	if !strings.Contains(stripANSI(m.View()), "📷 Photo sunset") {
		t.Fatalf("label missing:\n%s", stripANSI(m.View()))
	}
	for _, c := range src.calls {
		if strings.HasPrefix(c, "media:") {
			t.Fatal("downloaded media with images off")
		}
	}
}

func TestAvatarsOnlyInKittyMode(t *testing.T) {
	src := &fakeMedia{dir: t.TempDir()}
	writePNG(t, filepath.Join(src.dir, "full.png"), fill(64, 64, color.RGBA{0, 255, 0, 255}))

	m := mediaModel(t, src, termimg.ModeBlocks)
	m = drain(t, m, m.loadVisible())
	if len(src.calls) != 0 {
		t.Fatalf("blocks mode fetched avatars: %v", src.calls)
	}

	var out bytes.Buffer
	kitty, err := termimg.NewKitty(&out)
	if err != nil {
		t.Fatal(err)
	}
	defer kitty.Close()
	cmds := make(chan messages.Command, 10)
	km := New(cmds, []*messages.Conversation{{JID: "333@s.whatsapp.net", Name: "Bob", LastMsgTime: 200}},
		Options{SidebarWidth: 30, Images: termimg.ModeKitty, Kitty: kitty, Media: src})
	next, cmd := km.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	km = drain(t, next.(Model), cmd)
	if len(src.calls) != 1 || src.calls[0] != "avatar:333@s.whatsapp.net" {
		t.Fatalf("calls = %v", src.calls)
	}
	if !strings.Contains(km.View(), "\U0010EEEE") {
		t.Fatal("avatar placeholder not in list view")
	}
	if !strings.Contains(out.String(), "a=T") {
		t.Fatal("avatar image not transmitted")
	}
}

func msgKey(m Model, msg messages.Message) imgKey {
	meta, _ := msg.MediaMeta()
	cols, rows := m.mediaCells(meta, m.bubbleMaxInner(m.rightWidth()))
	return imgKey{imgMessage, msg.Id, cols, rows}
}

func TestMediaCellsKeepAspect(t *testing.T) {
	m := mediaModel(t, &fakeMedia{dir: t.TempDir()}, termimg.ModeBlocks)
	cols, rows := m.mediaCells(messages.MediaMeta{Type: messages.MediaImage, Width: 400, Height: 300}, 60)
	if cols != 37 || rows != 14 { // height-limited: 14 rows * 16px = 224px tall -> 299px wide
		t.Fatalf("image cells = %dx%d, want 37x14", cols, rows)
	}
	cols, rows = m.mediaCells(messages.MediaMeta{Type: messages.MediaSticker, Width: 512, Height: 512}, 60)
	if cols != 14 || rows != 7 {
		t.Fatalf("sticker cells = %dx%d, want 14x7", cols, rows)
	}
}

func TestAvatarLetterFallback(t *testing.T) {
	src := &fakeMedia{dir: t.TempDir(), noPic: true}
	var out bytes.Buffer
	kitty, err := termimg.NewKitty(&out)
	if err != nil {
		t.Fatal(err)
	}
	defer kitty.Close()
	chats := []*messages.Conversation{{JID: "333@s.whatsapp.net", Name: "Bob", LastMsgTime: 200}}
	m := New(make(chan messages.Command, 10), chats,
		Options{SidebarWidth: 30, Images: termimg.ModeKitty, Kitty: kitty, Media: src})
	m.img.cellW, m.img.cellH = 8, 16
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = drain(t, next.(Model), cmd)

	e := m.img.get(imgKey{imgAvatar, "333@s.whatsapp.net", avatarBigCols, avatarBigRows})
	if e == nil || e.state != imgReady {
		t.Fatalf("letter avatar not ready: %+v", e)
	}
	if !strings.Contains(m.View(), "\U0010EEEE") {
		t.Fatal("letter avatar not drawn as an image")
	}
	if n := strings.Count(out.String(), "a=T"); n != 1 {
		t.Fatalf("transmitted %d images, want 1 (letter only)", n)
	}
}

func TestAnimatedStickerPlaysInKitty(t *testing.T) {
	src := &fakeMedia{dir: t.TempDir(), mediaPath: "../termimg/testdata/anim_alpha_lossless.webp"}
	var out bytes.Buffer
	kitty, err := termimg.NewKitty(&out)
	if err != nil {
		t.Fatal(err)
	}
	defer kitty.Close()
	m := New(make(chan messages.Command, 10), []*messages.Conversation{{JID: "333@s.whatsapp.net", Name: "Bob", LastMsgTime: 200}},
		Options{SidebarWidth: 30, Images: termimg.ModeKitty, Kitty: kitty, Media: src})
	m.img.cellW, m.img.cellH = 8, 16
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter")

	blob, err := proto.Marshal(&waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		Width: proto.Uint32(96), Height: proto.Uint32(96), IsAnimated: proto.Bool(true)}})
	if err != nil {
		t.Fatal(err)
	}
	sticker := messages.Message{Id: "st1", ChatId: "333@s.whatsapp.net", ContactId: "333", Timestamp: 1700000000,
		Text: "[STICKER]", MediaType: messages.MediaSticker, Media: blob}
	next, cmd := m.Update(screenMsg{sticker})
	m = drain(t, next.(Model), cmd)

	e := m.img.get(msgKey(m, sticker))
	if e == nil || e.state != imgReady || !e.full {
		t.Fatalf("sticker entry = %+v", e)
	}
	if n := strings.Count(out.String(), "a=f,"); n != 5 {
		t.Fatalf("sent %d extra frames, want 5", n)
	}
	if !strings.Contains(out.String(), "s=3,v=1") {
		t.Fatal("animation not started")
	}
	v := stripANSI(m.View())
	if strings.Contains(v, "Sticker") || strings.Count(v, "╭") != 1 { // only the input box
		t.Fatalf("sticker should render as an image without a bubble:\n%s", v)
	}
}
