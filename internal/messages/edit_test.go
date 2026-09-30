package messages

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestCanEdit(t *testing.T) {
	now := uint64(time.Now().Unix())
	mine := Message{Id: "a", FromMe: true, Text: "hi", Timestamp: now, Status: StatusSent}
	cases := []struct {
		name string
		m    Message
		ok   bool
	}{
		{"own recent text", mine, true},
		{"someone else's", Message{Text: "hi", Timestamp: now}, false},
		{"too old", Message{FromMe: true, Text: "hi", Timestamp: now - 16*60, Status: StatusSent}, false},
		{"a photo", Message{FromMe: true, Text: "cap", MediaType: "image", Timestamp: now, Status: StatusSent}, false},
		{"deleted", Message{FromMe: true, Text: noteYouDeleted, Timestamp: now, Status: StatusSent}, false},
		{"not sent", Message{FromMe: true, Text: "hi", Timestamp: now, Status: StatusFailed}, false},
	}
	for _, c := range cases {
		if ok, why := CanEdit(c.m); ok != c.ok {
			t.Errorf("%s: CanEdit = %v (%s), want %v", c.name, ok, why, c.ok)
		}
	}
}

func TestEditOf(t *testing.T) {
	edit := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:           &waCommon.MessageKey{ID: proto.String("MSG1")},
		EditedMessage: &waE2E.Message{Conversation: proto.String("fixed typo")},
	}}
	if id, text, ok := editOf(edit); !ok || id != "MSG1" || text != "fixed typo" {
		t.Fatalf("editOf = %q %q %v", id, text, ok)
	}
	// with formatting / mentions it comes as extended text
	edit.ProtocolMessage.EditedMessage = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("*bold* now")}}
	if _, text, ok := editOf(edit); !ok || text != "*bold* now" {
		t.Fatalf("extended edit = %q %v", text, ok)
	}
	if _, _, ok := editOf(&waE2E.Message{Conversation: proto.String("plain")}); ok {
		t.Fatal("a normal message isn't an edit")
	}
}

func TestEditMessageInDB(t *testing.T) {
	db := newTestDB(t)
	for _, m := range []Message{
		{Id: "a", ChatId: "c@s.whatsapp.net", Text: "teh cat", Timestamp: 1, FromMe: true},
		{Id: "b", ChatId: "c@s.whatsapp.net", Text: noteDeleted, Timestamp: 2},
	} {
		if err := db.AddMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	if changed, err := db.EditMessage("a", "the cat"); err != nil || !changed {
		t.Fatalf("edit: %v %v", changed, err)
	}
	if changed, _ := db.EditMessage("b", "sneaky"); changed {
		t.Fatal("a deleted message was edited")
	}
	if changed, _ := db.EditMessage("missing", "x"); changed {
		t.Fatal("edited a message that doesn't exist")
	}
	msgs, err := db.GetMessages("c@s.whatsapp.net")
	if err != nil || len(msgs) != 2 {
		t.Fatalf("messages: %v %v", msgs, err)
	}
	if msgs[0].Text != "the cat" || !msgs[0].Edited {
		t.Fatalf("after edit: %+v", msgs[0])
	}
	if msgs[1].Text != noteDeleted || msgs[1].Edited {
		t.Fatalf("deleted message changed: %+v", msgs[1])
	}
	// the original arriving again (history sync) doesn't undo the edit
	_ = db.AddMessage(Message{Id: "a", ChatId: "c@s.whatsapp.net", Text: "teh cat", Timestamp: 1, FromMe: true})
	if msgs, _ := db.GetMessages("c@s.whatsapp.net"); msgs[0].Text != "the cat" {
		t.Fatalf("re-sync undid the edit: %q", msgs[0].Text)
	}
}
