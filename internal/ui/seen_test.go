package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

const seenJID = "u@s.whatsapp.net"

func seenModel(t *testing.T, unread uint16) (Model, chan messages.Command) {
	t.Helper()
	ch := make(chan messages.Command, 50)
	m := New(ch, []*messages.Conversation{
		{JID: seenJID, Name: "Mom", LastMsgTime: 9, Unread: unread},
		{JID: "d@s.whatsapp.net", Name: "Dad", LastMsgTime: 8},
	}, Options{SidebarWidth: 38, Images: termimg.ModeOff})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return next.(Model), ch
}

// sent returns the commands the model sent, running its pending cmds.
func sent(ch chan messages.Command, cmds ...tea.Cmd) []string {
	run(cmds)
	var out []string
	for {
		select {
		case c := <-ch:
			out = append(out, c.Name+" "+strings.Join(c.Params, " "))
		default:
			return out
		}
	}
}

func chatMsgs(from, n int) []messages.Message {
	var msgs []messages.Message
	for i := from; i < from+n; i++ {
		msgs = append(msgs, messages.Message{Id: fmt.Sprint("m", i), ChatId: seenJID, ContactId: seenJID,
			Timestamp: uint64(1700000000 + i*60), Text: fmt.Sprint("message ", i)})
	}
	return msgs
}

func TestOpeningMarksRead(t *testing.T) {
	m, ch := seenModel(t, 3)
	m, cmds := keys(t, m, "enter")
	if got := strings.Join(sent(ch, cmds...), ","); got != "select "+seenJID+",read "+seenJID {
		t.Fatalf("sent %s", got)
	}
	m, _ = keys(t, m, "backspace")
	if strings.Contains(stripANSI(m.View()), "unread") {
		t.Fatal("the badge should go right away")
	}
}

func TestMessageWhileWatchingIsRead(t *testing.T) {
	m, ch := seenModel(t, 0)
	m, cmds := keys(t, m, "enter")
	sent(ch, cmds...)
	// a message arrives in the open chat: the list update says unread 1
	next, cmd := m.Update(chatListMsg{{JID: seenJID, Name: "Mom", LastMsgTime: 10, Unread: 1}})
	m = next.(Model)
	if got := sent(ch, cmd); len(got) != 1 || got[0] != "read "+seenJID {
		t.Fatalf("sent %v", got)
	}
	// ...but not for a chat you aren't looking at
	m, _ = keys(t, m, "backspace")
	next, cmd = m.Update(chatListMsg{{JID: seenJID, Name: "Mom", LastMsgTime: 11, Unread: 1}})
	m = next.(Model)
	if got := sent(ch, cmd); len(got) != 0 {
		t.Fatalf("sent %v while in the list", got)
	}
	if !strings.Contains(stripANSI(m.View()), "1 unread") {
		t.Fatal("it's unread while you aren't looking")
	}
}

func TestUnfocusedWindowDoesntMarkRead(t *testing.T) {
	m, ch := seenModel(t, 2)
	next, _ := m.Update(tea.BlurMsg{})
	m = next.(Model)
	m, cmds := keys(t, m, "enter")
	if got := sent(ch, cmds...); len(got) != 1 {
		t.Fatalf("sent %v with the window in the background", got)
	}
	next, cmd := m.Update(tea.FocusMsg{})
	m = next.(Model)
	if got := sent(ch, cmd); len(got) != 1 || got[0] != "read "+seenJID {
		t.Fatalf("focus: sent %v", got)
	}
	_ = m
}

func TestReopenStartsAtNewest(t *testing.T) {
	m, _ := seenModel(t, 0)
	m, _ = keys(t, m, "enter")
	next, _ := m.Update(screenMsg(chatMsgs(0, 60)))
	m = next.(Model)
	m.vp.SetYOffset(0) // scrolled all the way up
	m, _ = keys(t, m, "backspace", "enter")
	if !m.vp.AtBottom() {
		t.Fatal("reopening should start at the newest message")
	}
}

func TestOlderHistoryKeepsYourPlace(t *testing.T) {
	m, _ := seenModel(t, 0)
	m, _ = keys(t, m, "enter")
	next, _ := m.Update(screenMsg(chatMsgs(100, 60)))
	m = next.(Model)
	m.vp.ScrollUp(20) // reading a bit further up
	top, _ := m.topVisible()
	// 50 older messages arrive from the phone
	next, _ = m.Update(screenMsg(append(chatMsgs(50, 50), chatMsgs(100, 60)...)))
	m = next.(Model)
	if now, _ := m.topVisible(); now != top {
		t.Fatalf("top message changed from %s to %s", top, now)
	}
}
