package messages

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestExtractMedia(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{"nil", nil, ""},
		{"text", &waE2E.Message{Conversation: proto.String("hi")}, ""},
		{"image", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Width: proto.Uint32(800)}}, MediaImage},
		{"sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}, MediaSticker},
		{"video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}, MediaVideo},
		{"gif", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{GifPlayback: proto.Bool(true)}}, MediaGIF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ, blob := extractMedia(tt.msg)
			if typ != tt.want {
				t.Fatalf("type = %q, want %q", typ, tt.want)
			}
			if (blob != nil) != (tt.want != "") {
				t.Fatalf("blob presence = %v", blob != nil)
			}
		})
	}
}

func TestMediaMeta(t *testing.T) {
	typ, blob := extractMedia(&waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Width: proto.Uint32(1280), Height: proto.Uint32(720),
			JPEGThumbnail: []byte{0xff, 0xd8},
			Caption:       proto.String("look"),
		},
	})
	meta, ok := Message{MediaType: typ, Media: blob}.MediaMeta()
	if !ok || meta.Type != MediaImage || meta.Width != 1280 || meta.Height != 720 || len(meta.Thumbnail) != 2 {
		t.Fatalf("meta = %+v ok=%v", meta, ok)
	}

	typ, blob = extractMedia(&waE2E.Message{StickerMessage: &waE2E.StickerMessage{IsAnimated: proto.Bool(true)}})
	if meta, ok := (Message{MediaType: typ, Media: blob}).MediaMeta(); !ok || !meta.Animated {
		t.Fatalf("sticker meta = %+v ok=%v", meta, ok)
	}

	if _, ok := (Message{Text: "hi"}).MediaMeta(); ok {
		t.Fatal("text message reported media")
	}
	if _, ok := (Message{MediaType: MediaImage, Media: []byte("garbage")}).MediaMeta(); ok {
		t.Fatal("corrupt media reported ok")
	}
}

func TestStorage_MediaFilledOnConflict(t *testing.T) {
	md := newTestDB(t)
	base := Message{Id: "m1", ChatId: "c", Timestamp: 10, Text: "[IMAGE]"}
	if err := md.AddMessage(base); err != nil {
		t.Fatal(err)
	}

	// Re-adding with media fills it in, without touching other columns.
	withMedia := base
	withMedia.Text = "changed"
	withMedia.MediaType, withMedia.Media = extractMedia(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{}})
	if err := md.AddMessage(withMedia); err != nil {
		t.Fatal(err)
	}
	got, err := md.GetMessage("m1")
	if err != nil {
		t.Fatal(err)
	}
	if got.MediaType != MediaImage || len(got.Media) == 0 || got.Text != "[IMAGE]" {
		t.Fatalf("after fill: %+v", got)
	}

	// Existing media is never overwritten.
	other := base
	other.MediaType, other.Media = MediaSticker, []byte{1}
	if err := md.AddMessage(other); err != nil {
		t.Fatal(err)
	}
	if got, _ := md.GetMessage("m1"); got.MediaType != MediaImage {
		t.Fatalf("media overwritten: %q", got.MediaType)
	}
}

func TestStorage_LatestAndOldest(t *testing.T) {
	md := newTestDB(t)
	for i, ts := range []uint64{30, 10, 50, 20, 40} {
		if err := md.AddMessage(Message{Id: string(rune('a' + i)), ChatId: "c", Timestamp: ts}); err != nil {
			t.Fatal(err)
		}
	}
	_ = md.AddMessage(Message{Id: "other", ChatId: "x", Timestamp: 1})

	latest, err := md.GetLatestMessages("c", 3)
	if err != nil {
		t.Fatal(err)
	}
	var ts []uint64
	for _, m := range latest {
		ts = append(ts, m.Timestamp)
	}
	if len(ts) != 3 || ts[0] != 30 || ts[1] != 40 || ts[2] != 50 {
		t.Fatalf("latest = %v, want [30 40 50]", ts)
	}

	oldest, err := md.GetOldestMessage("c")
	if err != nil || oldest.Timestamp != 10 {
		t.Fatalf("oldest = %+v err=%v", oldest, err)
	}
	if _, err := md.GetOldestMessage("empty"); err == nil {
		t.Fatal("expected error for empty chat")
	}
}
