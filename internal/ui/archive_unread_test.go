package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

func TestArchiveRow(t *testing.T) {
	m, _ := testModel(t)
	v := stripANSI(m.View())
	if !strings.Contains(v, "Archived") || !strings.Contains(v, "📦") {
		t.Fatalf("archive row missing:\n%s", v)
	}
	if m.selectedChat() == nil || m.selectedChat().Name != "Book Club" {
		t.Fatal("cursor should start on the newest chat")
	}
	m, _ = keys(t, m, "k", "enter")
	if !m.archive || len(m.visibleChats()) != 1 {
		t.Fatal("enter on the archive row should open the archive")
	}
	if strings.Contains(stripANSI(m.View()), "📦") {
		t.Fatal("the archive itself shouldn't show the archive row")
	}
	// open an archived chat, then A from inside it goes back to the inbox
	m, _ = keys(t, m, "enter")
	if m.screen != screenChat || m.current.Name != "Old" {
		t.Fatalf("archived chat not opened: %v", m.current)
	}
	m, _ = keys(t, m, "A")
	if m.archive || m.focus != paneList {
		t.Fatal("A in a chat should switch the list (and focus it)")
	}
	m, _ = keys(t, m, "A")
	if !m.archive {
		t.Fatal("A again should show the archive")
	}
}

func TestArchiveRowClick(t *testing.T) {
	m, _ := testModel(t)
	m, _ = click(t, m, 10, headerRows+1) // first item: the archive row
	if !m.archive {
		t.Fatal("clicking the archive row should open the archive")
	}
}

func unreadModel(t *testing.T, unread int) (Model, []messages.Message) {
	t.Helper()
	jid := "u@s.whatsapp.net"
	m := New(make(chan messages.Command, 10), []*messages.Conversation{{JID: jid, Name: "Mom", LastMsgTime: 9, Unread: uint16(unread)}},
		Options{SidebarWidth: 38, Images: termimg.ModeOff})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(Model)
	var msgs []messages.Message
	for i := 0; i < 40; i++ {
		msgs = append(msgs, messages.Message{Id: fmt.Sprint("m", i), ChatId: jid, ContactId: jid, FromMe: i%5 == 0 && i < 30,
			Timestamp: uint64(1700000000 + i*60), Text: fmt.Sprint("message ", i)})
	}
	m, _ = keys(t, m, "enter")
	next, _ = m.Update(screenMsg(msgs))
	return next.(Model), msgs
}

func TestOpenChatAtFirstUnread(t *testing.T) {
	m, _ := unreadModel(t, 12)
	// the last 12 incoming messages are m28..m39 (m25 is from you, m30+ all incoming)
	if m.unreadID != "m28" {
		t.Fatalf("first unread = %s, want m28", m.unreadID)
	}
	v := stripANSI(m.View())
	if !strings.Contains(v, "12 unread messages") {
		t.Fatalf("divider not visible:\n%s", v)
	}
	if !strings.Contains(v, "message 28") || strings.Contains(v, "message 20 ") {
		t.Fatalf("view should start at the unread messages:\n%s", v)
	}
	if m.vp.AtBottom() {
		t.Fatal("should not jump to the bottom when there are unread messages")
	}
	// a later refresh (receipts, new messages) doesn't move the divider
	next, _ := m.Update(screenMsg(m.msgs))
	if next.(Model).unreadID != "m28" {
		t.Fatal("divider moved on refresh")
	}
}

func TestMoreUnreadThanLoaded(t *testing.T) {
	// the phone's count covers messages not loaded here, so there's no
	// telling where they start: open at the newest instead of the oldest
	m, _ := unreadModel(t, 443)
	if m.unreadID != "" || !m.vp.AtBottom() || strings.Contains(stripANSI(m.View()), "unread messages") {
		t.Fatalf("unreadID %q, at bottom %v", m.unreadID, m.vp.AtBottom())
	}
}

func TestNoUnreadGoesToBottom(t *testing.T) {
	m, _ := unreadModel(t, 0)
	if m.unreadID != "" || !m.vp.AtBottom() || strings.Contains(stripANSI(m.View()), "unread message") {
		t.Fatal("a chat without unread messages opens at the bottom")
	}
}

func TestMentionsOfYouStandOut(t *testing.T) {
	jid := groupJID
	m := New(make(chan messages.Command, 10), []*messages.Conversation{
		{JID: jid, Name: "Hostel", LastMsgTime: 9, Unread: 3, Mentioned: true},
		{JID: "x@s.whatsapp.net", Name: "Mom", LastMsgTime: 8, Unread: 1},
	}, Options{SidebarWidth: 38, Images: termimg.ModeOff})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	list := stripANSI(m.View())
	if strings.Count(list, " @ ") != 1 {
		t.Fatalf("exactly the mentioned chat should show @:\n%s", list)
	}
	m, _ = keys(t, m, "enter")
	you := map[string]string{"918331840042": "You"}
	next, _ = m.Update(screenMsg{
		{Id: "a", ChatId: jid, ContactId: "1", ContactShort: "Arjun", Timestamp: 1700000000, Text: "@918331840042 are you in?", Mentions: you},
		{Id: "b", ChatId: jid, ContactId: "2", ContactShort: "Priya", Timestamp: 1700000100, Text: "anyone?"},
		{Id: "c", ChatId: jid, ContactId: "3", ContactShort: "Ravi", Timestamp: 1700000200, Text: "cc @918331840042", Mentions: you},
	})
	m = next.(Model)
	v := stripANSI(m.View())
	if strings.Count(v, "@ mentioned you") != 2 {
		t.Fatalf("mention labels:\n%s", v)
	}
	// @ jumps between them, newest first, wrapping
	m, _ = keys(t, m, "@")
	if m.mode != modeVisual || m.msgs[m.sel].Id != "c" {
		t.Fatalf("@ -> %s", m.msgs[m.sel].Id)
	}
	m, _ = keys(t, m, "@")
	if m.msgs[m.sel].Id != "a" {
		t.Fatalf("@ @ -> %s", m.msgs[m.sel].Id)
	}
	m, _ = keys(t, m, "@")
	if m.msgs[m.sel].Id != "c" {
		t.Fatal("@ should wrap to the newest mention")
	}
}
