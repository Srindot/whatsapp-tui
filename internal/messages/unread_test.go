package messages

import (
	"container/heap"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestCapUnreadAfterReplies(t *testing.T) {
	db := newTestDB(t)
	add := func(chat string, ts uint64, fromMe bool) {
		t.Helper()
		if err := db.AddMessage(Message{Id: chat + string(rune('a'+ts)), ChatId: chat, Timestamp: ts, FromMe: fromMe, Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	// replied after 3 of theirs, then 1 more came: 1 unread, not 7
	add("p@s.whatsapp.net", 1, false)
	add("p@s.whatsapp.net", 2, false)
	add("p@s.whatsapp.net", 3, false)
	add("p@s.whatsapp.net", 4, true)
	add("p@s.whatsapp.net", 5, false)
	// your message is the newest: nothing unread
	add("q@s.whatsapp.net", 1, false)
	add("q@s.whatsapp.net", 2, true)
	// you never wrote here: the phone's count stays
	add("g@g.us", 1, false)
	for jid, n := range map[string]uint16{"p@s.whatsapp.net": 7, "q@s.whatsapp.net": 3, "g@g.us": 40} {
		if err := db.UpsertConversation(Conversation{JID: jid, Unread: n, Mentioned: true, LastMsgTime: 9}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CapUnreadAfterReplies(); err != nil {
		t.Fatal(err)
	}
	unread, mentioned, err := db.UnreadCounts()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uint16{"p@s.whatsapp.net": 1, "q@s.whatsapp.net": 0, "g@g.us": 40}
	for jid, n := range want {
		if unread[jid] != n {
			t.Errorf("%s: unread %d, want %d", jid, unread[jid], n)
		}
	}
	if mentioned["q@s.whatsapp.net"] || !mentioned["g@g.us"] {
		t.Errorf("mentioned = %v", mentioned)
	}
}

func TestOwnMessageFromPhoneReadsTheChat(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	chat := types.NewJID("919000000001", types.DefaultUserServer)
	c := &Conversation{JID: chat.String(), Name: "Mom", LastMsgTime: 5, Unread: 4, Mentioned: true}
	heap.Push(&sm.priorityQueue, c)
	sm.convByJID[c.JID] = c
	eh := &eventHandler{sm: sm}
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat, IsFromMe: true},
			ID:            "MINE", Timestamp: time.Now(),
		},
		Message: &waE2E.Message{Conversation: proto.String("on my way")},
	}
	eh.processIncomingMessage(evt, "on my way", "on my way")
	if c.Unread != 0 || c.Mentioned {
		t.Fatalf("after your reply from the phone: unread %d mentioned %v", c.Unread, c.Mentioned)
	}
	// and theirs still counts
	evt.Info.IsFromMe, evt.Info.ID = false, "THEIRS"
	eh.processIncomingMessage(evt, "ok", "ok")
	if c.Unread != 1 {
		t.Fatalf("their message: unread %d", c.Unread)
	}
}

// Reading a chat on your phone sends a "read" receipt from your own account;
// it clears the chat here. (They used to be ignored.)
func TestReadOnPhoneReceiptClearsChat(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	chat := types.NewJID("919000000002", types.DefaultUserServer)
	c := &Conversation{JID: chat.String(), Name: "Dad", LastMsgTime: 5, Unread: 6, Mentioned: true}
	heap.Push(&sm.priorityQueue, c)
	sm.convByJID[c.JID] = c
	set := func(n uint16) {
		sm.mu.Lock()
		c.Unread, c.Mentioned = n, n > 0
		sm.mu.Unlock()
	}
	get := func() (uint16, bool) {
		sm.mu.RLock()
		defer sm.mu.RUnlock()
		return c.Unread, c.Mentioned
	}
	for _, typ := range []types.ReceiptType{types.ReceiptTypeRead, types.ReceiptTypeReadSelf} {
		set(6)
		sm.handleReceipt(&events.Receipt{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat, IsFromMe: true},
			MessageIDs:    []types.MessageID{"X"},
			Type:          typ,
		})
		if n, mentioned := get(); n != 0 || mentioned {
			t.Fatalf("%s receipt from your phone: unread %d mentioned %v", typ, n, mentioned)
		}
	}
	// someone else's read receipt (on your message) doesn't touch your unread
	set(3)
	sm.handleReceipt(&events.Receipt{
		MessageSource: types.MessageSource{Chat: chat, Sender: chat},
		MessageIDs:    []types.MessageID{"Y"},
		Type:          types.ReceiptTypeRead,
	})
	if n, _ := get(); n != 3 {
		t.Fatalf("their receipt changed your unread: %d", n)
	}
}
