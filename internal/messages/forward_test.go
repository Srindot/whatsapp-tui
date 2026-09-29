package messages

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"go.mau.fi/whatsmeow/proto/waE2E"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestForwardProto(t *testing.T) {
	// text
	pm, err := forwardProto(Message{Text: "hello"})
	if err != nil || pm.GetExtendedTextMessage().GetText() != "hello" ||
		!pm.GetExtendedTextMessage().GetContextInfo().GetIsForwarded() {
		t.Fatalf("text: %v %v", pm, err)
	}
	// forwarding a forwarded message bumps the score
	pm, _ = forwardProto(Message{Text: "hi", Forwarded: true})
	if pm.GetExtendedTextMessage().GetContextInfo().GetForwardingScore() != 2 {
		t.Fatal("score not bumped")
	}
	// image keeps the uploaded file and caption
	typ, blob := extractMedia(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		DirectPath: proto.String("/v/t62/abc"), MediaKey: []byte{1, 2}, Caption: proto.String("old")}})
	pm, err = forwardProto(Message{Text: "[IMAGE] sunset", MediaType: typ, Media: blob})
	im := pm.GetImageMessage()
	if err != nil || im.GetDirectPath() != "/v/t62/abc" || im.GetCaption() != "sunset" || !im.GetContextInfo().GetIsForwarded() {
		t.Fatalf("image: %+v %v", im, err)
	}
	// sticker
	typ, blob = extractMedia(&waE2E.Message{StickerMessage: &waE2E.StickerMessage{DirectPath: proto.String("/s")}})
	if pm, err := forwardProto(Message{Text: "[STICKER]", MediaType: typ, Media: blob}); err != nil || pm.GetStickerMessage() == nil {
		t.Fatalf("sticker: %v", err)
	}
	// media we never got download info for
	for _, m := range []Message{{Text: "[IMAGE] caption"}, {Text: "[STICKER]"}, {Text: "[DOCUMENT] a.pdf"}} {
		if _, err := forwardProto(m); err == nil {
			t.Errorf("%q should not be forwardable", m.Text)
		}
	}
	// texts that merely start with a bracket are fine
	if _, err := forwardProto(Message{Text: "[SELL] 6in1 veg pizza"}); err != nil {
		t.Fatalf("bracket text: %v", err)
	}
	// an image that never finished uploading
	typ, blob = extractMedia(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{}})
	if _, err := forwardProto(Message{Text: "[IMAGE]", MediaType: typ, Media: blob}); err == nil {
		t.Fatal("unuploaded image forwarded")
	}
}

func TestForwardWhileOfflineStoresFailedCopies(t *testing.T) {
	sm, _ := lidSM(t)
	own, _ := types.ParseJID("919999999999@s.whatsapp.net")
	sm.client.Store.ID = &own
	addTestMsg(t, sm.db, Message{Id: "orig", ChatId: testPN, ContactId: testPN, Timestamp: 1, Text: "look at this"})

	err := sm.ForwardMessage(context.Background(), "orig", []string{"111@s.whatsapp.net", "222@g.us"})
	if err == nil {
		t.Fatal("expected an error while offline")
	}
	for _, chat := range []string{"111@s.whatsapp.net", "222@g.us"} {
		msgs, _ := sm.db.GetLatestMessages(chat, 5)
		if len(msgs) != 1 || !msgs[0].Forwarded || msgs[0].Text != "look at this" || msgs[0].Status != StatusFailed {
			t.Fatalf("%s: %+v", chat, msgs)
		}
	}
}

func TestSendDocumentOffline(t *testing.T) {
	sm, _ := lidSM(t)
	own, _ := types.ParseJID("919999999999@s.whatsapp.net")
	sm.client.Store.ID = &own
	// xdg reads the environment once; reload so the cache goes to a temp dir
	// (and not your real ~/.cache).
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	if !strings.HasPrefix(config.GetCacheDir(), os.Getenv("XDG_CACHE_HOME")) {
		t.Fatal("cache dir not redirected")
	}
	dir := t.TempDir()
	path := dir + "/report.pdf"
	if err := os.WriteFile(path, []byte("%PDF-1.4"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sm.SendDocument(context.Background(), testPN, path, "Q3 numbers"); err == nil {
		t.Fatal("expected an error while offline")
	}
	msgs, _ := sm.db.GetLatestMessages(testPN, 5)
	if len(msgs) != 1 || msgs[0].Status != StatusFailed || msgs[0].MediaType != MediaDoc ||
		!strings.HasPrefix(msgs[0].Text, "[DOCUMENT] report.pdf") {
		t.Fatalf("stored = %+v", msgs)
	}
	if err := sm.SendDocument(context.Background(), testPN, dir, ""); err == nil || !strings.Contains(err.Error(), "folder") {
		t.Fatalf("folder: %v", err)
	}
}
