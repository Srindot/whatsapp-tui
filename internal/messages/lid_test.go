package messages

import (
	"container/heap"
	"context"
	"io"
	"os"
	"testing"
	"time"

	signallogger "go.mau.fi/libsignal/logger"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

const (
	testPN  = "919030304583@s.whatsapp.net"
	testLID = "74766857801951@lid"
)

// lidSM is a session manager with a real (in-memory) whatsmeow store that
// knows testLID belongs to testPN.
func lidSM(t *testing.T) (*SessionManager, *MockUiHandler) {
	t.Helper()
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite3", "file:"+t.Name()+"?mode=memory&cache=shared&_foreign_keys=on", waLog.Noop)
	if err != nil {
		t.Fatal(err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	device.LIDs = container.LIDMap // set on pairing; this device is unpaired
	lid, _ := types.ParseJID(testLID)
	pn, _ := types.ParseJID(testPN)
	if err := device.LIDs.PutLIDMapping(ctx, lid, pn); err != nil {
		t.Fatal(err)
	}
	ui := NewMockUiHandler()
	sm := &SessionManager{uiHandler: ui, db: newTestDB(t), convByJID: map[string]*Conversation{},
		client: whatsmeow.NewClient(device, waLog.Noop)}
	heap.Init(&sm.priorityQueue)
	return sm, ui
}

func TestCanonicalSource(t *testing.T) {
	sm, _ := lidSM(t)
	ctx := context.Background()
	lid, _ := types.ParseJID(testLID)
	pn, _ := types.ParseJID(testPN)

	src := types.MessageSource{Chat: lid, Sender: lid}
	sm.canonicalSource(ctx, &src)
	if src.Chat != pn || src.Sender != pn {
		t.Fatalf("dm: chat %v sender %v, want %v", src.Chat, src.Sender, pn)
	}

	// Unknown LID: fall back to the alternative address in the message.
	other, _ := types.ParseJID("111@lid")
	otherPN, _ := types.ParseJID("91222@s.whatsapp.net")
	src = types.MessageSource{Chat: other, Sender: other, SenderAlt: otherPN}
	sm.canonicalSource(ctx, &src)
	if src.Chat != otherPN || src.Sender != otherPN {
		t.Fatalf("alt: chat %v sender %v", src.Chat, src.Sender)
	}

	// Groups keep their JID; LID senders are mapped.
	group, _ := types.ParseJID("120363@g.us")
	src = types.MessageSource{Chat: group, Sender: lid, IsGroup: true}
	sm.canonicalSource(ctx, &src)
	if src.Chat != group || src.Sender != pn {
		t.Fatalf("group: chat %v sender %v", src.Chat, src.Sender)
	}
}

func TestMergeLIDChats(t *testing.T) {
	sm, ui := lidSM(t)
	for _, c := range []*Conversation{
		{JID: testPN, Name: "Aneesh Sambu", LastMsgTime: 100, Preview: "old", Unread: 1},
		{JID: testLID, Name: "Aneesh Sambu", LastMsgTime: 200, Preview: "new", Unread: 2},
		{JID: "999@lid", Name: "Unknown", LastMsgTime: 50}, // no mapping: kept
	} {
		heap.Push(&sm.priorityQueue, c)
		sm.convByJID[c.JID] = c
		if err := sm.db.UpsertConversation(*c); err != nil {
			t.Fatal(err)
		}
	}
	addTestMsg(t, sm.db, Message{Id: "a", ChatId: testPN, ContactId: testPN, Timestamp: 100, Text: "old"})
	addTestMsg(t, sm.db, Message{Id: "b", ChatId: testLID, ContactId: testLID, Timestamp: 200, Text: "new"})
	if err := sm.db.SetReaction("a", testLID, "👍", 1); err != nil {
		t.Fatal(err)
	}

	sm.mergeLIDChats(context.Background())

	if sm.convByJID[testLID] != nil || sm.convByJID["999@lid"] == nil {
		t.Fatal("wrong chats merged")
	}
	c := sm.convByJID[testPN]
	if c.LastMsgTime != 200 || c.Preview != "new" || c.Unread != 3 {
		t.Fatalf("merged chat = %+v", c)
	}
	if len(sm.priorityQueue) != 2 {
		t.Fatalf("queue has %d chats, want 2", len(sm.priorityQueue))
	}
	msgs, _ := sm.db.GetLatestMessages(testPN, 10)
	if len(msgs) != 2 || msgs[1].ContactId != testPN {
		t.Fatalf("messages after merge: %+v", msgs)
	}
	if r, _ := sm.db.GetReactions([]string{"a"}); len(r["a"]) != 1 || r["a"][0].Sender != testPN {
		t.Fatalf("reaction sender not merged: %+v", r)
	}
	convs, _ := sm.db.GetConversations()
	for _, c := range convs {
		if c.JID == testLID {
			t.Fatal("LID conversation still in the database")
		}
	}
	if len(ui.ChatLists) != 1 {
		t.Fatal("chat list not refreshed")
	}

	// Running again changes nothing.
	sm.mergeLIDChats(context.Background())
	if len(ui.ChatLists) != 1 {
		t.Fatal("second merge refreshed the list")
	}
}

func TestArchiveEventsMoveChats(t *testing.T) {
	sm, ui := lidSM(t)
	c := &Conversation{JID: testPN, Name: "Aneesh Sambu", LastMsgTime: 100}
	heap.Push(&sm.priorityQueue, c)
	sm.convByJID[c.JID] = c

	yes, no := true, false
	lid, _ := types.ParseJID(testLID) // phone reports the chat by LID
	sm.setChatFlags(lid, &yes, nil)
	if !c.IsArchived {
		t.Fatal("archive event not applied")
	}
	sm.setChatFlags(lid, nil, &yes)
	if !c.IsPinned || !c.IsArchived {
		t.Fatalf("pin event: %+v", c)
	}
	sm.setChatFlags(lid, &no, nil)
	if c.IsArchived {
		t.Fatal("unarchive not applied")
	}
	convs, _ := sm.db.GetConversations()
	if len(convs) != 1 || convs[0].IsArchived || !convs[0].IsPinned {
		t.Fatalf("not persisted: %+v", convs)
	}

	// The list is pushed once after the burst, not per event.
	deadline := time.Now().Add(2 * time.Second)
	for ui.ChatListCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
	if n := ui.ChatListCount(); n != 1 {
		t.Fatalf("list pushed %d times, want 1", n)
	}
}

func TestResolveMentions(t *testing.T) {
	sm, _ := lidSM(t)
	own, _ := types.ParseJID("918331840042@s.whatsapp.net")
	sm.client.Store.ID = &own
	msgs := []Message{
		{Text: "hey @918331840042 look"},               // you
		{Text: "ask @74766857801951 about it"},         // a LID we can map to a number
		{Text: "cc @12345678 and @918331840042 again"}, // unknown number, you
		{Text: "no mentions, email a@b.c"},
	}
	sm.resolveMentions(msgs)
	if msgs[0].Mentions["918331840042"] != "You" {
		t.Fatalf("self: %v", msgs[0].Mentions)
	}
	if got := msgs[1].Mentions["74766857801951"]; got != "+91 90303 04583" {
		t.Fatalf("LID: %q", got)
	}
	if _, ok := msgs[2].Mentions["12345678"]; ok || msgs[2].Mentions["918331840042"] != "You" {
		t.Fatalf("unknown: %v", msgs[2].Mentions)
	}
	if msgs[3].Mentions != nil {
		t.Fatal("mentions found where there are none")
	}
}

func TestMentionsMe(t *testing.T) {
	sm, _ := lidSM(t)
	own, _ := types.ParseJID("918331840042@s.whatsapp.net")
	lid, _ := types.ParseJID("144989690589358@lid")
	sm.client.Store.ID, sm.client.Store.LID = &own, lid
	mk := func(jids ...string) *waE2E.Message {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("hi"), ContextInfo: &waE2E.ContextInfo{MentionedJID: jids}}}
	}
	if !sm.mentionsMe(mk("918331840042@s.whatsapp.net")) || !sm.mentionsMe(mk("111@lid", "144989690589358@lid")) {
		t.Fatal("mention of you not detected")
	}
	if sm.mentionsMe(mk("919999999999@s.whatsapp.net")) || sm.mentionsMe(&waE2E.Message{Conversation: proto.String("x")}) {
		t.Fatal("false mention")
	}
	// the flag is stored with the chat
	if err := sm.db.UpsertConversation(Conversation{JID: "g@g.us", Name: "G", LastMsgTime: 1, Mentioned: true}); err != nil {
		t.Fatal(err)
	}
	convs, _ := sm.db.GetConversations()
	if len(convs) != 1 || !convs[0].Mentioned {
		t.Fatalf("mentioned flag not stored: %+v", convs)
	}
}

func TestSignalLogsDontReachStdout(t *testing.T) {
	sm := &SessionManager{uiHandler: NewMockUiHandler()}
	sm.quietSignalLogs()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	signallogger.Error("Unable to get or create message keys: ", "received message with old counter")
	signallogger.Warning("something")
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if len(out) != 0 {
		t.Fatalf("signal library printed to stdout: %q", out)
	}
}

// Reading a chat on your phone clears it here too.
func TestReadOnPhoneClearsUnread(t *testing.T) {
	sm, _ := lidSM(t)
	c := &Conversation{JID: testPN, Name: "Aneesh Sambu", LastMsgTime: 100, Unread: 5, Mentioned: true}
	heap.Push(&sm.priorityQueue, c)
	sm.convByJID[c.JID] = c
	lid, _ := types.ParseJID(testLID) // phone reports the chat by LID
	sm.setChatRead(lid, true)
	if c.Unread != 0 || c.Mentioned {
		t.Fatalf("still unread: %+v", c)
	}
	sm.setChatRead(lid, false) // "mark as unread"
	if c.Unread != 1 {
		t.Fatalf("mark unread: %+v", c)
	}
	if convs, _ := sm.db.GetConversations(); len(convs) != 1 || convs[0].Unread != 1 {
		t.Fatalf("not persisted: %+v", convs)
	}
}
