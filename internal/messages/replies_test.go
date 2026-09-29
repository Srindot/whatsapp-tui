package messages

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestQuoteOf(t *testing.T) {
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("yes!"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("Q1"),
			Participant:   proto.String("91999@s.whatsapp.net"),
			QuotedMessage: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("dinner?")}},
		},
	}}
	var m Message
	quoteOf(&m, msg)
	if m.QuotedID != "Q1" || m.QuotedSender != "91999@s.whatsapp.net" || m.QuotedText != "[IMAGE] dinner?" {
		t.Fatalf("quote = %+v", m)
	}
	var plain Message
	quoteOf(&plain, &waE2E.Message{Conversation: proto.String("hi")})
	if plain.QuotedID != "" {
		t.Fatal("plain message got a quote")
	}
}

func TestReactionsStorage(t *testing.T) {
	md := newTestDB(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(md.SetReaction("m1", "a@s.whatsapp.net", "👍", 100))
	must(md.SetReaction("m1", "", "❤️", 110))
	must(md.SetReaction("m1", "a@s.whatsapp.net", "😂", 120)) // change
	must(md.SetReaction("m1", "a@s.whatsapp.net", "😮", 90))  // stale, ignored
	must(md.SetReaction("m2", "b@s.whatsapp.net", "🙏", 100))
	must(md.SetReaction("m2", "b@s.whatsapp.net", "", 130)) // removed

	got, err := md.GetReactions([]string{"m1", "m2", "m3"})
	must(err)
	if len(got["m1"]) != 2 || got["m1"][0].Emoji != "❤️" || got["m1"][1].Emoji != "😂" {
		t.Fatalf("m1 = %+v", got["m1"])
	}
	if len(got["m2"]) != 0 {
		t.Fatalf("m2 = %+v, want removed", got["m2"])
	}
}

func TestLegacyReactionRowsHidden(t *testing.T) {
	md := newTestDB(t)
	addTestMsg(t, md, Message{Id: "1", ChatId: "c", Timestamp: 1, Text: "hello"})
	addTestMsg(t, md, Message{Id: "2", ChatId: "c", Timestamp: 2, Text: "[REACTION] 👍"})
	msgs, err := md.GetLatestMessages("c", 10)
	if err != nil || len(msgs) != 1 || msgs[0].Text != "hello" {
		t.Fatalf("msgs = %+v err %v", msgs, err)
	}
}

func TestQuotedStoredAndKept(t *testing.T) {
	md := newTestDB(t)
	addTestMsg(t, md, Message{Id: "r", ChatId: "c", Timestamp: 1, Text: "ok", QuotedID: "q", QuotedSender: "s", QuotedText: "orig"})
	addTestMsg(t, md, Message{Id: "r", ChatId: "c", Timestamp: 1, Text: "ok"}) // re-sync without quote
	m, err := md.GetMessage("r")
	if err != nil || m.QuotedID != "q" || m.QuotedText != "orig" {
		t.Fatalf("quote lost: %+v", m)
	}
}

func TestFormatPhoneAndPlaceholders(t *testing.T) {
	if got := formatPhone("919876543210"); got != "+91 98765 43210" {
		t.Errorf("india = %q", got)
	}
	if got := formatPhone("14155552671"); got != "+1 415 555 2671" {
		t.Errorf("us = %q", got)
	}
	if got := formatPhone("447911123456"); got != "+447911123456" {
		t.Errorf("other = %q", got)
	}
	for _, n := range []string{"", "919876543210", "+91∙∙∙∙∙∙∙∙44", "+91 98765 43210", "Group Chat"} {
		if !isPlaceholderName(n, "919876543210") {
			t.Errorf("%q should be a placeholder", n)
		}
	}
	for _, n := range []string{"Hari Shankar", "~ Hari", "😀 Priya"} {
		if isPlaceholderName(n, "919876543210") {
			t.Errorf("%q is a real name", n)
		}
	}
}

func TestMediaFileName(t *testing.T) {
	mk := func(pm *waE2E.Message) Message {
		typ, blob := extractMedia(pm)
		return Message{Id: "3EB0ABCDEF123456", Timestamp: 1790000000, MediaType: typ, Media: blob}
	}
	tests := []struct {
		msg  Message
		want string
	}{
		{mk(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")}}), "IMG-20260921-EF123456.jpg"},
		{mk(&waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}), "STK-20260921-EF123456.webp"},
		{mk(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4")}}), "VID-20260921-EF123456.mp4"},
		{mk(&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("../../etc/Report.pdf")}}), "Report.pdf"},
		{mk(&waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus")}}), "AUD-20260921-EF123456.ogg"},
	}
	for _, tt := range tests {
		if got := mediaFileName(tt.msg); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
		if strings.Contains(mediaFileName(tt.msg), "/") {
			t.Errorf("path separator in %q", mediaFileName(tt.msg))
		}
	}
}
