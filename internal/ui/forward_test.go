package ui

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeForwarder struct {
	mu    sync.Mutex
	calls []string // "msgID>jid,jid"
}

func (f *fakeForwarder) ForwardMessage(_ context.Context, id string, to []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id+">"+strings.Join(to, ","))
	return nil
}

func forwardModel(t *testing.T) (Model, *fakeForwarder) {
	t.Helper()
	fw := &fakeForwarder{}
	chats := []*messages.Conversation{
		{JID: groupJID, Name: "Hostel", LastMsgTime: 300},
		{JID: "91111@s.whatsapp.net", Name: "Arjun", LastMsgTime: 200},
		{JID: "91333@s.whatsapp.net", Name: "Old friend", LastMsgTime: 100, IsArchived: true},
		{JID: "91444@s.whatsapp.net", Name: "Zara", LastMsgTime: 0},            // saved, never messaged
		{JID: "91555@s.whatsapp.net", Name: "+91 55555 55555", LastMsgTime: 0}, // just a number
	}
	m := New(make(chan messages.Command, 20), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff,
		Actions: &fakeActions{}, Forwarder: fw})
	m.cmdline.Cursor.SetMode(cursor.CursorStatic)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	next, _ = m.Update(screenMsg(groupMsgs()))
	return next.(Model), fw
}

func TestForwardTargets(t *testing.T) {
	m, _ := forwardModel(t)
	m, _ = keys(t, m, "v", "f")
	if m.fwd == nil {
		t.Fatal("f did not open the forward picker")
	}
	var names []string
	for _, c := range m.forwardTargets() {
		names = append(names, c.Name)
	}
	// recent chats (archived too) first, then saved contacts; bare numbers left out
	if got := strings.Join(names, ","); got != "Hostel,Arjun,Old friend,Zara" {
		t.Fatalf("targets = %s", got)
	}
	v := stripANSI(m.View())
	for _, want := range []string{"Forward to…", "count me in", "FORWARD"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
}

func TestForwardToOneChat(t *testing.T) {
	m, fw := forwardModel(t)
	m, _ = keys(t, m, "v", "k", "f") // forward Priya's "yes!"
	m, _ = keys(t, m, "z", "a")      // filter: Zara
	if targets := m.forwardTargets(); len(targets) != 1 || targets[0].Name != "Zara" {
		t.Fatalf("filter: %v", targets)
	}
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if len(fw.calls) != 1 || fw.calls[0] != "m2>91444@s.whatsapp.net" {
		t.Fatalf("calls = %v", fw.calls)
	}
	if m.fwd != nil || m.mode != modeNormal || m.notice != "Forwarded to Zara" {
		t.Fatalf("after send: fwd=%v mode=%d notice=%q", m.fwd != nil, m.mode, m.notice)
	}
}

func TestForwardToSeveral(t *testing.T) {
	m, fw := forwardModel(t)
	m, _ = keys(t, m, "v", "f")
	m, _ = keys(t, m, " ") // Hostel, cursor moves to Arjun
	m, _ = press(t, m, tea.KeyCtrlN)
	m, _ = keys(t, m, " ") // Old friend
	if !strings.Contains(stripANSI(m.View()), "2 selected") {
		t.Fatal("selection count not shown")
	}
	for i := 0; i < 3; i++ { // back up to Hostel (space also moved down)
		m, _ = press(t, m, tea.KeyCtrlP)
	}
	m, _ = keys(t, m, " ") // untick Hostel
	m, _ = keys(t, m, " ") // tick Arjun
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if len(fw.calls) != 1 || fw.calls[0] != "m3>91333@s.whatsapp.net,91111@s.whatsapp.net" {
		t.Fatalf("calls = %v", fw.calls)
	}
	if m.notice != "Forwarded to Old friend and Arjun" {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestForwardEscCancels(t *testing.T) {
	m, fw := forwardModel(t)
	m, _ = keys(t, m, "v", "f")
	m, _ = press(t, m, tea.KeyEsc)
	if m.fwd != nil || m.mode != modeVisual || len(fw.calls) != 0 {
		t.Fatalf("esc: fwd=%v mode=%d calls=%v", m.fwd != nil, m.mode, fw.calls)
	}
}

func TestHintsDontOverflowTheScreen(t *testing.T) {
	m, _ := forwardModel(t)
	check := func(name string) {
		t.Helper()
		if lines := strings.Count(m.View(), "\n") + 1; lines != 40 {
			t.Fatalf("%s: view has %d lines, want 40", name, lines)
		}
	}
	m, _ = keys(t, m, "v", "f")
	check("forward picker")
	m, _ = press(t, m, tea.KeyEsc)
	m, _ = press(t, m, tea.KeyEsc)
	m, _ = keys(t, m, "/", "y", "e")
	check("chat search")
}
