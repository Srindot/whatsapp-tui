package messages

import (
	"container/heap"
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestBuildDeleteForMe(t *testing.T) {
	group, _ := types.ParseJID("120363@g.us")
	dm, _ := types.ParseJID("919876543210@s.whatsapp.net")
	sender, _ := types.ParseJID("911111@s.whatsapp.net")
	ts := time.Unix(1700000000, 0)
	tests := []struct {
		chat   types.JID
		fromMe bool
		want   []string
	}{
		{group, false, []string{"deleteMessageForMe", "120363@g.us", "ID1", "0", "911111@s.whatsapp.net"}},
		{group, true, []string{"deleteMessageForMe", "120363@g.us", "ID1", "1", "0"}},
		{dm, false, []string{"deleteMessageForMe", "919876543210@s.whatsapp.net", "ID1", "0", "0"}},
	}
	for _, tt := range tests {
		p := buildDeleteForMe(tt.chat, sender, "ID1", tt.fromMe, ts)
		if p.Type != appstate.WAPatchRegularHigh || len(p.Mutations) != 1 {
			t.Fatalf("patch = %+v", p)
		}
		mu := p.Mutations[0]
		if len(mu.Index) != 5 {
			t.Fatalf("index %v", mu.Index)
		}
		for i := range tt.want {
			if mu.Index[i] != tt.want[i] {
				t.Fatalf("index %v, want %v", mu.Index, tt.want)
			}
		}
		if mu.Value.GetDeleteMessageForMeAction().GetMessageTimestamp() != 1700000000 {
			t.Fatal("timestamp missing")
		}
	}
}

func TestCanDeleteForEveryone(t *testing.T) {
	now := uint64(time.Now().Unix())
	tests := []struct {
		m    Message
		want bool
	}{
		{Message{FromMe: true, Timestamp: now - 3600, Status: StatusRead}, true},
		{Message{FromMe: true, Timestamp: now - 3*24*3600, Status: StatusRead}, false}, // too old
		{Message{FromMe: false, Timestamp: now}, false},                                // not yours
		{Message{FromMe: true, Timestamp: now, Status: StatusFailed}, false},           // never sent
		{Message{FromMe: true, Timestamp: now, Text: noteYouDeleted}, false},           // already deleted
	}
	for i, tt := range tests {
		if got := CanDeleteForEveryone(tt.m); got != tt.want {
			t.Errorf("case %d: %v, want %v", i, got, tt.want)
		}
	}
}

func TestDeleteStorageAndRevoke(t *testing.T) {
	sm, ui := lidSM(t)
	addTestMsg(t, sm.db, Message{Id: "a", ChatId: "c", Timestamp: 1, Text: "[IMAGE] pic", MediaType: MediaImage, Media: []byte{1}})
	addTestMsg(t, sm.db, Message{Id: "b", ChatId: "c", Timestamp: 2, Text: "hello"})
	_ = sm.db.SetReaction("a", "x", "👍", 1)

	// someone deletes "a" for everyone
	sm.currentReceiver = "c"
	sm.handleRevoke("c", &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key: &waCommon.MessageKey{ID: proto.String("a")}}, false)
	m, _ := sm.db.GetMessage("a")
	if m.Text != noteDeleted || m.MediaType != "" || len(m.Media) != 0 {
		t.Fatalf("revoked = %+v", m)
	}
	if r, _ := sm.db.GetReactions([]string{"a"}); len(r["a"]) != 0 {
		t.Fatal("reactions kept on a deleted message")
	}
	time.Sleep(10 * time.Millisecond)
	ui.mu.Lock()
	screens := len(ui.Screens)
	ui.mu.Unlock()
	if screens == 0 {
		t.Fatal("open chat not refreshed")
	}

	if err := sm.db.DeleteMessage("b"); err != nil {
		t.Fatal(err)
	}
	if msgs, _ := sm.db.GetLatestMessages("c", 5); len(msgs) != 1 {
		t.Fatalf("%d messages left", len(msgs))
	}
}

func TestDeleteFailedMessageOffline(t *testing.T) {
	sm, _ := lidSM(t)
	m := Message{Id: "f", ChatId: testPN, FromMe: true, Timestamp: 1, Text: "oops", Status: StatusFailed}
	addTestMsg(t, sm.db, m)
	if err := sm.DeleteForMe(context.Background(), m); err != nil {
		t.Fatalf("a message that never sent should delete offline: %v", err)
	}
	if _, err := sm.db.GetMessage("f"); err == nil {
		t.Fatal("still there")
	}
	sent := Message{Id: "s", ChatId: testPN, FromMe: true, Timestamp: 1, Text: "hi", Status: StatusSent}
	if err := sm.DeleteForMe(context.Background(), sent); err == nil {
		t.Fatal("deleting a sent message needs a connection")
	}
}

func TestRemoveChatLocally(t *testing.T) {
	sm, ui := lidSM(t)
	for _, c := range []*Conversation{{JID: testPN, Name: "A", LastMsgTime: 2}, {JID: "b@s.whatsapp.net", Name: "B", LastMsgTime: 1}} {
		heap.Push(&sm.priorityQueue, c)
		sm.convByJID[c.JID] = c
		_ = sm.db.UpsertConversation(*c)
	}
	addTestMsg(t, sm.db, Message{Id: "1", ChatId: testPN, Timestamp: 1, Text: "x"})
	sm.removeChatLocally(testPN)
	if sm.convByJID[testPN] != nil || len(sm.priorityQueue) != 1 {
		t.Fatal("chat still in memory")
	}
	if msgs, _ := sm.db.GetLatestMessages(testPN, 5); len(msgs) != 0 {
		t.Fatal("messages kept")
	}
	convs, _ := sm.db.GetConversations()
	if len(convs) != 1 || convs[0].JID != "b@s.whatsapp.net" {
		t.Fatalf("conversations = %+v", convs)
	}
	if ui.ChatListCount() != 1 {
		t.Fatal("list not refreshed")
	}
}
