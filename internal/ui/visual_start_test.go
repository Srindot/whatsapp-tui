package ui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// longChat is a chat with 60 messages, scrolled to the bottom.
func longChat(t *testing.T) Model {
	t.Helper()
	jid := "u@s.whatsapp.net"
	m := New(make(chan messages.Command, 10), []*messages.Conversation{{JID: jid, Name: "Mom", LastMsgTime: 9}},
		Options{SidebarWidth: 38, Images: termimg.ModeOff})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	var msgs []messages.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs, messages.Message{Id: fmt.Sprint("m", i), ChatId: jid, ContactId: jid,
			Timestamp: uint64(1700000000 + i*60), Text: fmt.Sprint("message ", i)})
	}
	next, _ = m.Update(screenMsg(msgs))
	return next.(Model)
}

func TestVisualStartsWhereYouScrolled(t *testing.T) {
	m := longChat(t)
	m, _ = keys(t, m, "v")
	if m.sel != 59 {
		t.Fatalf("at the bottom: sel %d, want the newest", m.sel)
	}
	m, _ = keys(t, m, "esc")

	m.vp.SetYOffset(0) // scrolled up to the oldest messages
	y := m.vp.YOffset
	m, _ = keys(t, m, "v")
	if m.sel >= 30 {
		t.Fatalf("scrolled up: sel %d jumped toward the newest", m.sel)
	}
	// the selected message is on screen and the view didn't move
	if m.vp.YOffset != y {
		t.Fatalf("view moved from %d to %d", y, m.vp.YOffset)
	}
	var shown bool
	for _, sp := range m.msgSpans {
		if sp.idx == m.sel && sp.start >= m.vp.YOffset && sp.end < m.vp.YOffset+m.vp.Height {
			shown = true
		}
	}
	if !shown {
		t.Fatal("selected message isn't on screen")
	}
}

func TestVisualGgAndG(t *testing.T) {
	m := longChat(t)
	m, _ = keys(t, m, "v", "g", "g")
	if m.sel != 0 || m.vp.YOffset != 0 {
		t.Fatalf("gg: sel %d offset %d", m.sel, m.vp.YOffset)
	}
	m, _ = keys(t, m, "G")
	if m.sel != 59 || !m.vp.AtBottom() {
		t.Fatalf("G: sel %d at bottom %v", m.sel, m.vp.AtBottom())
	}
}
