package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/skratchdot/open-golang/open"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

func mouseModel(t *testing.T, chats int) Model {
	t.Helper()
	var cs []*messages.Conversation
	for i := 0; i < chats; i++ {
		cs = append(cs, &messages.Conversation{JID: fmt.Sprintf("%d@s.whatsapp.net", 100+i), Name: fmt.Sprintf("Chat %02d", i),
			LastMsgTime: int64(1000 - i)})
	}
	m := New(make(chan messages.Command, 20), cs, Options{SidebarWidth: 38, Images: termimg.ModeOff})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	return next.(Model)
}

func click(t *testing.T, m Model, x, y int) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return next.(Model), cmd
}

func wheel(t *testing.T, m Model, x, y int, up bool) Model {
	t.Helper()
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
	return next.(Model)
}

func TestClickOpensChat(t *testing.T) {
	m := mouseModel(t, 30)
	// rows 0-1 are the header; each chat is 3 rows: the third chat is at 8-10
	m, _ = click(t, m, 20, 9)
	if m.screen != screenChat || m.current == nil || m.current.Name != "Chat 02" {
		t.Fatalf("opened %v", m.current)
	}
	// clicking in the sidebar opens another; clicking the header does nothing
	m, _ = click(t, m, 10, 3)
	if m.current.Name != "Chat 00" {
		t.Fatalf("sidebar click opened %s", m.current.Name)
	}
	m, _ = click(t, m, 10, 0)
	if m.current.Name != "Chat 00" {
		t.Fatal("header click changed the chat")
	}
	// clicks in the message area don't open anything
	m, _ = click(t, m, 80, 9)
	if m.current.Name != "Chat 00" {
		t.Fatal("message-area click changed the chat")
	}
}

func TestWheelScrollsListAndMessages(t *testing.T) {
	m := mouseModel(t, 30)
	for i := 0; i < 3; i++ {
		m = wheel(t, m, 20, 10, false)
	}
	if m.listOffset != 3 {
		t.Fatalf("list offset %d, want 3", m.listOffset)
	}
	// after scrolling, a click maps to the chat actually shown there
	m, _ = click(t, m, 20, 3)
	if m.current == nil || m.current.Name != "Chat 03" {
		t.Fatalf("clicked %v", m.current)
	}
	var msgs []messages.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs, messages.Message{Id: fmt.Sprint(i), ChatId: m.current.JID, Timestamp: uint64(1700000000 + i), Text: fmt.Sprint("msg ", i)})
	}
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)
	bottom := m.vp.YOffset
	m = wheel(t, m, 80, 10, true)
	if m.vp.YOffset != bottom-wheelLines {
		t.Fatalf("messages scrolled %d -> %d", bottom, m.vp.YOffset)
	}
	// wheel over the sidebar scrolls the list, not the messages
	off := m.vp.YOffset
	m = wheel(t, m, 10, 10, false)
	if m.vp.YOffset != off || m.listOffset != 4 {
		t.Fatalf("sidebar wheel: vp %d->%d, list %d", off, m.vp.YOffset, m.listOffset)
	}
}

func TestMouseIgnoredUnderOverlays(t *testing.T) {
	m := mouseModel(t, 5)
	m, _ = keys(t, m, "?")
	m, _ = click(t, m, 20, 3)
	if m.screen != screenList || !m.showHelp {
		t.Fatal("click went through the help screen")
	}
}

func TestClickOpensLinks(t *testing.T) {
	var opened []string
	openURL = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURL = open.Start }()

	long := "https://www.linkedin.com/jobs/view/4470560160/?refId=abcdefghijkl&trackingId=xyz"
	m := mouseModel(t, 3)
	m, _ = keys(t, m, "enter")
	next, _ := m.Update(screenMsg{
		{Id: "1", ChatId: m.current.JID, ContactId: "x", Timestamp: 1700000000, Text: "short https://example.com/a here"},
		{Id: "2", ChatId: m.current.JID, ContactId: "x", Timestamp: 1700000060, Text: "job: " + long},
	})
	m = next.(Model)

	// find where text is drawn on screen (column in cells)
	find := func(needle string) (int, int) {
		for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			if i := strings.Index(line, needle); i >= 0 {
				return ansi.StringWidth(line[:i]), y
			}
		}
		t.Fatalf("%q not on screen", needle)
		return 0, 0
	}
	for _, tc := range []struct{ needle, want string }{
		{"example.com", "https://example.com/a"},
		{"https://www.link", long}, // first line of the wrapped link
		{"refId", long},            // a continuation line
	} {
		opened = nil
		x, y := find(tc.needle)
		m2, cmd := click(t, m, x+2, y)
		drain(t, m2, cmd)
		if len(opened) != 1 || opened[0] != tc.want {
			t.Fatalf("click on %q opened %v, want %s", tc.needle, opened, tc.want)
		}
	}
	// plain text next to a link opens nothing
	opened = nil
	x, y := find("short")
	m2, cmd := click(t, m, x+1, y)
	drain(t, m2, cmd)
	if len(opened) != 0 {
		t.Fatalf("click on plain text opened %v", opened)
	}
}

func TestURLAtColumn(t *testing.T) {
	line := "ab\x1b[1m\x1b]8;;https://x.io\x1b\\link\x1b]8;;\x1b\\\x1b[0m end ✨ \x1b]8;;https://y.io\x07yy\x1b]8;;\x07"
	for col, want := range map[int]string{0: "", 2: "https://x.io", 5: "https://x.io", 6: "", 11: "", 13: "", 14: "https://y.io", 15: "https://y.io", 16: ""} {
		if got := urlAtColumn(line, col); got != want {
			t.Errorf("col %d: %q, want %q", col, got, want)
		}
	}
}
