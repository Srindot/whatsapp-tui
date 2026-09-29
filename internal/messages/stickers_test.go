package messages

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"golang.org/x/image/webp"
	"google.golang.org/protobuf/proto"
)

func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

func TestMakeSticker(t *testing.T) {
	needFFmpeg(t)
	// a wide red image: the sticker should be letterboxed with transparency
	img := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{220, 40, 60, 255})
		}
	}
	src := filepath.Join(t.TempDir(), "in.png")
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	_ = os.WriteFile(src, b.Bytes(), 0o600)

	data, err := MakeSticker(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > stickerMaxBytes {
		t.Fatalf("sticker is %d bytes", len(data))
	}
	out, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if out.Bounds().Dx() != 512 || out.Bounds().Dy() != 512 {
		t.Fatalf("size %v", out.Bounds())
	}
	if _, _, _, a := out.At(256, 10).RGBA(); a != 0 {
		t.Fatalf("top padding not transparent (alpha %d)", a)
	}
	if r, _, _, a := out.At(256, 256).RGBA(); a == 0 || r < 0x9000 {
		t.Fatal("picture missing from the middle")
	}
}

func TestMakeGIF(t *testing.T) {
	needFFmpeg(t)
	// a 10-frame animated GIF with an odd size
	g := &gif.GIF{}
	pal := color.Palette{color.Black, color.White, color.RGBA{235, 111, 146, 255}}
	for i := 0; i < 10; i++ {
		fr := image.NewPaletted(image.Rect(0, 0, 301, 201), pal)
		for x := i * 20; x < i*20+40 && x < 301; x++ {
			for y := 80; y < 120; y++ {
				fr.SetColorIndex(x, y, 2)
			}
		}
		g.Image, g.Delay = append(g.Image, fr), append(g.Delay, 10)
	}
	src := filepath.Join(t.TempDir(), "anim.gif")
	f, _ := os.Create(src)
	if err := gif.EncodeAll(f, g); err != nil {
		t.Fatal(err)
	}
	f.Close()

	mp4, thumb, w, h, secs, err := MakeGIF(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(mp4) < 100 || string(mp4[4:8]) != "ftyp" {
		t.Fatal("not an mp4")
	}
	if w%2 != 0 || h%2 != 0 || w != 300 {
		t.Fatalf("size %dx%d, want even and 300 wide", w, h)
	}
	if len(thumb) < 100 || secs != 1 {
		t.Fatalf("thumb %d bytes, %d s", len(thumb), secs)
	}
}

func TestRecentMediaDedupes(t *testing.T) {
	sm := &SessionManager{uiHandler: NewMockUiHandler(), db: newTestDB(t)}
	sticker := func(hash byte, path string) []byte {
		_, blob := extractMedia(&waE2E.Message{StickerMessage: &waE2E.StickerMessage{
			FileSHA256: []byte{hash}, DirectPath: proto.String(path)}})
		return blob
	}
	for i, st := range []struct {
		hash byte
		path string
	}{{1, "/a"}, {2, "/b"}, {1, "/a"}, {3, ""}, {4, "/d"}} {
		addTestMsg(t, sm.db, Message{Id: fmt.Sprint("s", i), ChatId: "c", Timestamp: uint64(100 + i),
			Text: "[STICKER]", MediaType: MediaSticker, Media: sticker(st.hash, st.path)})
	}
	got, err := sm.RecentMedia(context.Background(), MediaSticker, 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, m.Id)
	}
	// newest first; s0 duplicates s2; s3 was never uploaded
	if fmt.Sprint(ids) != "[s4 s2 s1]" {
		t.Fatalf("recents = %v", ids)
	}
}
