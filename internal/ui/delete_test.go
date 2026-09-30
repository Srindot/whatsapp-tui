package ui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeDeleter struct {
	mu  sync.Mutex
	did []string // "me:id", "all:id", "chat:jid"
}

func (f *fakeDeleter) rec(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.did = append(f.did, s)
	return nil
}
func (f *fakeDeleter) DeleteForMe(_ context.Context, m messages.Message) error {
	return f.rec("me:" + m.Id)
}
func (f *fakeDeleter) DeleteForEveryone(_ context.Context, m messages.Message) error {
	return f.rec("all:" + m.Id)
}
func (f *fakeDeleter) DeleteChat(_ context.Context, jid string) error { return f.rec("chat:" + jid) }

func deleteModel(t *testing.T) (Model, *fakeDeleter) {
	t.Helper()
	fd := &fakeDeleter{}
	chats := []*messages.Conversation{
		{JID: groupJID, Name: "Hostel", LastMsgTime: 3},
		{JID: "91111@s.whatsapp.net", Name: "Arjun", LastMsgTime: 2},
	}
	m := New(make(chan messages.Command, 10), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff, Deleter: fd})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	now := uint64(time.Now().Unix())
	next, _ = m.Update(screenMsg{
		{Id: "old", ChatId: groupJID, ContactId: "1", ContactShort: "Arjun", Timestamp: now - 600, Text: "someone else's"},
		{Id: "mine", ChatId: groupJID, FromMe: true, Timestamp: now - 60, Text: "my recent message", Status: messages.StatusRead},
	})
	return next.(Model), fd
}

func TestDeleteMessageForMeAndEveryone(t *testing.T) {
	m, fd := deleteModel(t)
	m, _ = keys(t, m, "v", "d") // own recent message
	v := stripANSI(m.View())
	if m.confirm == nil || !strings.Contains(v, "Delete this message?") || !strings.Contains(v, "e for everyone") {
		t.Fatalf("confirm not shown:\n%s", v)
	}
	m, cmds := keys(t, m, "e")
	m = drain(t, m, tea.Batch(cmds...))
	if len(fd.did) != 1 || fd.did[0] != "all:mine" || m.notice != "Deleted for everyone" {
		t.Fatalf("did %v notice %q", fd.did, m.notice)
	}

	// someone else's message: only "for me" is offered, and e does nothing
	m, _ = keys(t, m, "k", "d")
	if strings.Contains(stripANSI(m.View()), "e for everyone") {
		t.Fatal("'for everyone' offered on someone else's message")
	}
	m, _ = keys(t, m, "e")
	if len(fd.did) != 1 || m.notice != "Not deleted" {
		t.Fatalf("e on someone else's message: did %v notice %q", fd.did, m.notice)
	}
	m, _ = keys(t, m, "d")
	m, cmd := press(t, m, tea.KeyEnter)
	drain(t, m, cmd)
	if len(fd.did) != 2 || fd.did[1] != "me:old" {
		t.Fatalf("did %v", fd.did)
	}
}

func TestDeleteCancelled(t *testing.T) {
	m, fd := deleteModel(t)
	m, _ = keys(t, m, "v", "d")
	m, _ = press(t, m, tea.KeyEsc)
	if m.confirm != nil || len(fd.did) != 0 || m.mode != modeVisual {
		t.Fatalf("esc: confirm=%v did=%v mode=%d", m.confirm != nil, fd.did, m.mode)
	}
}

func TestDeleteChat(t *testing.T) {
	m, fd := deleteModel(t)
	m, _ = keys(t, m, "h", "d") // from the sidebar, on the open chat
	if v := stripANSI(m.View()); !strings.Contains(v, "Delete chat “Hostel”") {
		t.Fatalf("confirm:\n%s", v)
	}
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if len(fd.did) != 1 || fd.did[0] != "chat:"+groupJID {
		t.Fatalf("did %v", fd.did)
	}
	if m.screen != screenList || m.current != nil || m.notice != "Chat deleted" {
		t.Fatalf("after deleting the open chat: screen %d current %v notice %q", m.screen, m.current, m.notice)
	}
	// d in the list on another chat, then anything but enter cancels
	m, _ = keys(t, m, "d", "x")
	if len(fd.did) != 1 {
		t.Fatal("x should cancel")
	}
}

func TestDeletedMessageLooksDeleted(t *testing.T) {
	m, _ := deleteModel(t)
	next, _ := m.Update(screenMsg{{Id: "x", ChatId: groupJID, ContactId: "1", ContactShort: "Arjun", Timestamp: 1700000000, Text: "🚫 This message was deleted"}})
	m = next.(Model)
	if !strings.Contains(stripANSI(m.View()), "🚫 This message was deleted") {
		t.Fatal("note missing")
	}
	if !strings.Contains(m.View(), "\x1b[3") { // italic
		t.Skip("colour profile without styles")
	}
}
