package messages

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestStatusOnlyMovesForward(t *testing.T) {
	md := newTestDB(t)
	addTestMsg(t, md, Message{Id: "m", ChatId: "c", FromMe: true, Timestamp: 1, Text: "hi", Status: StatusPending})
	get := func() int {
		t.Helper()
		m, err := md.GetMessage("m")
		if err != nil {
			t.Fatal(err)
		}
		return m.Status
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(md.SetStatus("m", StatusSent))
	if ok, _ := md.AdvanceStatus("m", StatusRead); !ok || get() != StatusRead {
		t.Fatalf("read not applied: %d", get())
	}
	if ok, _ := md.AdvanceStatus("m", StatusDelivered); ok || get() != StatusRead {
		t.Fatal("late delivery receipt moved read back")
	}
	must(md.SetStatus("m", StatusSent)) // sender finishing after the receipt
	if get() != StatusRead {
		t.Fatal("SetStatus overwrote a receipt")
	}
	// re-syncing the message keeps the best status
	addTestMsg(t, md, Message{Id: "m", ChatId: "c", FromMe: true, Timestamp: 1, Text: "hi", Status: StatusSent})
	if get() != StatusRead {
		t.Fatal("upsert moved status back")
	}
}

func TestStatusFromHistory(t *testing.T) {
	tests := map[waWeb.WebMessageInfo_Status]int{
		waWeb.WebMessageInfo_ERROR:        StatusUnknown,
		waWeb.WebMessageInfo_PENDING:      StatusUnknown,
		waWeb.WebMessageInfo_SERVER_ACK:   StatusSent,
		waWeb.WebMessageInfo_DELIVERY_ACK: StatusDelivered,
		waWeb.WebMessageInfo_READ:         StatusRead,
		waWeb.WebMessageInfo_PLAYED:       StatusPlayed,
	}
	for in, want := range tests {
		if got := statusFromHistory(in); got != want {
			t.Errorf("%v -> %d, want %d", in, got, want)
		}
	}
}

func TestReceiptsUpdateStatus(t *testing.T) {
	sm, _ := lidSM(t)
	addTestMsg(t, sm.db, Message{Id: "a", ChatId: testPN, FromMe: true, Timestamp: 1, Text: "x", Status: StatusSent})
	addTestMsg(t, sm.db, Message{Id: "b", ChatId: testPN, FromMe: true, Timestamp: 2, Text: "y", Status: StatusSent})
	chat, _ := types.ParseJID(testLID) // receipts may come addressed by LID

	sm.handleReceipt(&events.Receipt{MessageSource: types.MessageSource{Chat: chat}, MessageIDs: []types.MessageID{"a", "b"}, Type: types.ReceiptTypeDelivered})
	sm.handleReceipt(&events.Receipt{MessageSource: types.MessageSource{Chat: chat}, MessageIDs: []types.MessageID{"a"}, Type: types.ReceiptTypeRead})
	// our own other device reading doesn't count
	sm.handleReceipt(&events.Receipt{MessageSource: types.MessageSource{Chat: chat, IsFromMe: true}, MessageIDs: []types.MessageID{"b"}, Type: types.ReceiptTypeRead})
	sm.handleReceipt(&events.Receipt{MessageSource: types.MessageSource{Chat: chat}, MessageIDs: []types.MessageID{"b"}, Type: types.ReceiptTypeReadSelf})

	a, _ := sm.db.GetMessage("a")
	b, _ := sm.db.GetMessage("b")
	if a.Status != StatusRead || b.Status != StatusDelivered {
		t.Fatalf("a=%d b=%d, want read, delivered", a.Status, b.Status)
	}
}

func TestSendWhileOfflineIsKeptAsFailed(t *testing.T) {
	sm, ui := lidSM(t)
	own, _ := types.ParseJID("919999999999@s.whatsapp.net")
	sm.client.Store.ID = &own // paired, but not connected
	sm.currentReceiver = testPN
	jid, _ := types.ParseJID(testPN)

	stored, err := sm.sendTracked(context.Background(), jid, &waE2E.Message{Conversation: proto.String("hello")}, Message{Text: "hello"}, "hello")
	if err == nil {
		t.Fatal("expected an error while offline")
	}
	m, gerr := sm.db.GetMessage(stored.Id)
	if gerr != nil || m.Status != StatusFailed || m.Text != "hello" || !m.FromMe {
		t.Fatalf("stored = %+v err %v", m, gerr)
	}
	if len(ui.Messages) != 1 || ui.Messages[0].Status != StatusPending {
		t.Fatalf("pending message not shown first: %+v", ui.Messages)
	}
	time.Sleep(250 * time.Millisecond) // debounced refresh shows the failure
	ui.mu.Lock()
	n := len(ui.Screens)
	ui.mu.Unlock()
	if n == 0 {
		t.Fatal("chat not refreshed after the failure")
	}

	if err := sm.ResendMessage(context.Background(), stored.Id); err == nil {
		t.Fatal("resend while offline should fail")
	}
	if m, _ := sm.db.GetMessage(stored.Id); m.Status != StatusFailed {
		t.Fatalf("status after failed resend = %d", m.Status)
	}
}

func TestSelfChatIsRead(t *testing.T) {
	sm, _ := lidSM(t)
	own, _ := types.ParseJID("918331840042@s.whatsapp.net")
	sm.client.Store.ID = &own
	self := "918331840042@s.whatsapp.net"
	if !sm.isSelfChat(self) || sm.isSelfChat(testPN) || sm.isSelfChat("918331840042-1600000000@g.us") {
		t.Fatal("self chat detection wrong")
	}
	if sm.sentStatus(self) != StatusRead || sm.sentStatus(testPN) != StatusSent {
		t.Fatal("sentStatus wrong")
	}
	msgs := []Message{
		{FromMe: true, Status: StatusUnknown}, {FromMe: true, Status: StatusSent},
		{FromMe: true, Status: StatusPending}, {FromMe: true, Status: StatusFailed},
		{FromMe: false, Status: StatusUnknown},
	}
	sm.selfChatRead(self, msgs)
	want := []int{StatusRead, StatusRead, StatusPending, StatusFailed, StatusUnknown}
	for i, m := range msgs {
		if m.Status != want[i] {
			t.Errorf("msg %d: status %d, want %d", i, m.Status, want[i])
		}
	}
	other := []Message{{FromMe: true, Status: StatusSent}}
	sm.selfChatRead(testPN, other)
	if other[0].Status != StatusSent {
		t.Fatal("other chats must not be marked read")
	}
}
