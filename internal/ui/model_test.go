package ui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

func testModel(t *testing.T) (Model, chan messages.Command) {
	t.Helper()
	cmds := make(chan messages.Command, 10)
	chats := []*messages.Conversation{
		{JID: "111@s.whatsapp.net", Name: "Alice", LastMsgTime: 100, Preview: "hi"},
		{JID: "222@g.us", Name: "Book Club", LastMsgTime: 300, Unread: 2},
		{JID: "333@s.whatsapp.net", Name: "Bob", LastMsgTime: 200, IsPinned: true},
		{JID: "444@s.whatsapp.net", Name: "Old", LastMsgTime: 50, IsArchived: true},
		{JID: "555@s.whatsapp.net", Name: "Never messaged", LastMsgTime: 0},
	}
	m := New(cmds, chats, Options{SidebarWidth: 30, PaintBackground: true})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Model), cmds
}

func keys(t *testing.T, m Model, ks ...string) (Model, []tea.Cmd) {
	t.Helper()
	var cmds []tea.Cmd
	for _, k := range ks {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "ctrl+d":
			msg = tea.KeyMsg{Type: tea.KeyCtrlD}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, cmd := m.Update(msg)
		m = next.(Model)
		cmds = append(cmds, cmd)
	}
	return m, cmds
}

// run executes cmds (which send to the backend channel) synchronously.
func run(cmds []tea.Cmd) {
	for _, c := range cmds {
		if c == nil {
			continue
		}
		if b, ok := c().(tea.BatchMsg); ok {
			run(b)
		}
	}
}

func TestChatOrder(t *testing.T) {
	m, _ := testModel(t)
	var got []string
	for _, c := range m.visibleChats() {
		got = append(got, c.Name)
	}
	// newest first; archived and never-messaged contacts hidden
	if want := "Book Club,Bob,Alice"; strings.Join(got, ",") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}

func TestNavigation(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantCursor int
		wantScreen screen
		wantFocus  pane
		wantMode   mode
	}{
		// item 0 is the "Archived" row; the cursor starts on the newest chat (1)
		{"start", nil, 1, screenList, paneList, modeNormal},
		{"j moves down", []string{"j"}, 2, screenList, paneList, modeNormal},
		{"j clamps", []string{"j", "j", "j", "j"}, 3, screenList, paneList, modeNormal},
		{"k reaches the archive row", []string{"k"}, 0, screenList, paneList, modeNormal},
		{"k clamps", []string{"k", "k"}, 0, screenList, paneList, modeNormal},
		{"G bottom", []string{"G"}, 3, screenList, paneList, modeNormal},
		{"gg top", []string{"G", "g", "g"}, 0, screenList, paneList, modeNormal},
		{"enter opens chat", []string{"j", "enter"}, 2, screenChat, paneMessages, modeNormal},
		{"l opens chat", []string{"l"}, 1, screenChat, paneMessages, modeNormal},
		{"h focuses sidebar", []string{"enter", "h"}, 1, screenChat, paneList, modeNormal},
		{"sidebar j/l opens next", []string{"enter", "h", "j", "l"}, 2, screenChat, paneMessages, modeNormal},
		{"backspace goes back", []string{"enter", "backspace"}, 1, screenList, paneList, modeNormal},
		{"backspace from sidebar", []string{"enter", "h", "backspace"}, 1, screenList, paneList, modeNormal},
		{"R focuses the box", []string{"enter", "R"}, 1, screenChat, paneMessages, modeText},
		{"R then i types", []string{"enter", "R", "i"}, 1, screenChat, paneMessages, modeInsert},
		{"i alone doesn't type", []string{"enter", "i"}, 1, screenChat, paneMessages, modeNormal},
		{"esc: insert to box", []string{"enter", "R", "i", "esc"}, 1, screenChat, paneMessages, modeText},
		{"esc esc leaves the box", []string{"enter", "R", "i", "esc", "esc"}, 1, screenChat, paneMessages, modeNormal},
		{"j types in insert", []string{"enter", "R", "i", "j"}, 1, screenChat, paneMessages, modeInsert},
		{": enters command", []string{":"}, 1, screenList, paneList, modeCommand},
		{"enter on the archive row opens the archive", []string{"k", "enter"}, 0, screenList, paneList, modeNormal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := testModel(t)
			m, _ = keys(t, m, tt.keys...)
			if m.cursor != tt.wantCursor || m.screen != tt.wantScreen || m.focus != tt.wantFocus || m.mode != tt.wantMode {
				t.Fatalf("got cursor=%d screen=%d focus=%d mode=%d, want %d %d %d %d",
					m.cursor, m.screen, m.focus, m.mode, tt.wantCursor, tt.wantScreen, tt.wantFocus, tt.wantMode)
			}
		})
	}
}

func TestOpenChatSelectsAndSends(t *testing.T) {
	m, ch := testModel(t)
	m, cmds := keys(t, m, "enter")
	run(cmds)
	if c := <-ch; c.Name != "select" || c.Params[0] != "222@g.us" {
		t.Fatalf("got %+v, want select 222@g.us", c)
	}
	// it had unread messages: opening it marks them read
	if c := <-ch; c.Name != "read" || c.Params[0] != "222@g.us" {
		t.Fatalf("got %+v, want read 222@g.us", c)
	}

	m, _ = keys(t, m, "R", "i", "h", "e", "y", " ", "y", "o")
	m, cmds = keys(t, m, "enter")
	run(cmds)
	c := <-ch
	if c.Name != "send" || len(c.Params) != 2 || c.Params[0] != "222@g.us" || c.Params[1] != "hey yo" {
		t.Fatalf("got %+v, want send to 222@g.us with one text param", c)
	}
	if m.compose.Value() != "" {
		t.Fatalf("compose not cleared: %q", m.compose.Value())
	}
}

func TestFilter(t *testing.T) {
	m, _ := testModel(t)
	m, _ = keys(t, m, "/", "a", "l")
	if n := len(m.visibleChats()); n != 1 || m.visibleChats()[0].Name != "Alice" {
		t.Fatalf("filter 'al' gave %d chats", n)
	}
	m, _ = keys(t, m, "esc")
	if len(m.visibleChats()) != 3 || m.mode != modeNormal {
		t.Fatalf("esc should clear filter")
	}
}

func TestMessagesForOtherChatIgnored(t *testing.T) {
	m, _ := testModel(t)
	m, _ = keys(t, m, "j", "enter") // Bob
	next, _ := m.Update(screenMsg{{ChatId: "111@s.whatsapp.net", Text: "stale"}})
	m = next.(Model)
	if len(m.msgs) != 0 {
		t.Fatal("stale screen for another chat was applied")
	}
	next, _ = m.Update(newMessageMsg{ChatId: "333@s.whatsapp.net", Text: "hello"})
	m = next.(Model)
	if len(m.msgs) != 1 {
		t.Fatal("message for open chat not appended")
	}
}

func TestListUpdateKeepsSelection(t *testing.T) {
	m, _ := testModel(t)
	m, _ = keys(t, m, "j", "j") // Alice
	next, _ := m.Update(chatListMsg{
		{JID: "111@s.whatsapp.net", Name: "Alice", LastMsgTime: 999},
		{JID: "222@g.us", Name: "Book Club", LastMsgTime: 300},
		{JID: "333@s.whatsapp.net", Name: "Bob", LastMsgTime: 200, IsPinned: true},
	})
	m = next.(Model)
	if c := m.selectedChat(); c == nil || c.Name != "Alice" {
		t.Fatalf("selection moved to %v", c)
	}
}

func TestStripTags(t *testing.T) {
	if got := stripTags("[red]Usage:[-] send [IMAGE] [::b]x[-:-:-]"); got != "Usage: send [IMAGE] x" {
		t.Fatalf("got %q", got)
	}
}

func TestViewRenders(t *testing.T) {
	m, _ := testModel(t)
	if v := m.View(); !strings.Contains(v, "Book Club") || !strings.Contains(v, "NORMAL") {
		t.Fatal("list view missing content")
	}
	m, _ = keys(t, m, "j", "enter")
	next, _ := m.Update(screenMsg{
		{ChatId: "333@s.whatsapp.net", ContactId: "333", ContactShort: "Bob", Timestamp: 1700000000, Text: "hello there"},
		{ChatId: "333@s.whatsapp.net", FromMe: true, Timestamp: 1700000060, Text: "hi bob"},
	})
	m = next.(Model)
	v := m.View()
	for _, want := range []string{"hello there", "hi bob", "Alice", "Bob"} {
		if !strings.Contains(v, want) {
			t.Fatalf("chat view missing %q", want)
		}
	}
	for i, line := range strings.Split(v, "\n") {
		if w := len([]rune(stripANSI(line))); w > 100 {
			t.Fatalf("line %d is %d cells wide", i, w)
		}
	}
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func TestArchiveList(t *testing.T) {
	m, _ := testModel(t)
	if in, ar, _ := m.chatCounts(); in != 3 || ar != 1 {
		t.Fatalf("counts = %d, %d; want 3, 1", in, ar)
	}
	m, _ = keys(t, m, "A")
	if v := m.visibleChats(); len(v) != 1 || v[0].Name != "Old" || !m.archive {
		t.Fatalf("archive view shows %v", v)
	}
	if !strings.Contains(stripANSI(m.View()), "Archived 1") {
		t.Fatal("archive tab not shown")
	}
	// backspace leaves the archive before it would do anything else
	m, _ = keys(t, m, "backspace")
	if m.archive || len(m.visibleChats()) != 3 {
		t.Fatal("backspace did not return to the inbox")
	}
	m, _ = keys(t, m, "A", "enter", "backspace")
	if !m.archive || m.screen != screenList {
		t.Fatal("going back from an archived chat should stay in the archive")
	}
	m, _ = keys(t, m, ":", "i", "n", "b", "o", "x", "enter")
	if m.archive {
		t.Fatal(":inbox did not leave the archive")
	}
}

func TestPrettyTags(t *testing.T) {
	tests := map[string]string{
		"[IMAGE]":           "📷 Photo",
		"[IMAGE] beach day": "📷 Photo beach day",
		"[STICKER]":         "✨ Sticker",
		"[SELL] pizza":      "[SELL] pizza",
		"plain":             "plain",
	}
	for in, want := range tests {
		if got := prettyTags(in); got != want {
			t.Errorf("prettyTags(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBubbleRounded(t *testing.T) {
	m, _ := testModel(t)
	m, _ = keys(t, m, "j", "enter")
	next, _ := m.Update(screenMsg{{ChatId: "333@s.whatsapp.net", ContactId: "333", Timestamp: 1700000000, Text: "hey"}})
	m = next.(Model)
	v := stripANSI(m.View())
	for _, want := range []string{"╭", "╰", "hey"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in\n%s", want, v)
		}
	}
	// text and time share the bubble's middle line
	found := false
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "│ hey") && strings.Contains(l, ":") {
			found = true
		}
	}
	if !found {
		t.Fatalf("text and time not on one line:\n%s", v)
	}
}

func TestHelpScreenCompleteAndScrolls(t *testing.T) {
	m, _ := testModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 220, Height: 60})
	m = next.(Model)
	m, _ = keys(t, m, "?")
	v := stripANSI(m.View())
	for _, want := range []string{"Chat list", "In a chat", "Insert (typing)", "Visual (v)", "Search",
		"Trays & panels", "Commands", "search messages in ALL chats", "forward", "retry a message",
		":sticker", ":attach", "shift+enter", "Your messages"} {
		if !strings.Contains(v, want) {
			t.Fatalf("help missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "j/k scroll · press") {
		t.Fatal("a big screen shouldn't need scrolling")
	}

	// a small terminal scrolls
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	v = stripANSI(m.View())
	if !strings.Contains(v, "j/k scroll · press") || strings.Contains(v, ":logout") {
		t.Fatalf("small screen should start at the top and offer scrolling:\n%s", v)
	}
	m, _ = keys(t, m, "G")
	if v = stripANSI(m.View()); !strings.Contains(v, ":logout") {
		t.Fatalf("G should reach the end:\n%s", v)
	}
	if lines := strings.Count(m.View(), "\n") + 1; lines != 24 {
		t.Fatalf("help overflows: %d lines", lines)
	}
	m, _ = keys(t, m, "esc")
	if m.showHelp || m.helpScroll != 0 {
		t.Fatal("esc should close help and reset scroll")
	}
	// :help opens the same screen
	m, _ = keys(t, m, ":", "h", "e", "l", "p")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !next.(Model).showHelp {
		t.Fatal(":help did not open help")
	}
}

func TestFilterFindsContactsAndOpens(t *testing.T) {
	ch := make(chan messages.Command, 10)
	chats := []*messages.Conversation{
		{JID: "919876543210@s.whatsapp.net", Name: "Hari Shankar", LastMsgTime: 300},
		{JID: "918438018376@s.whatsapp.net", Name: "~ Hari Kumar", LastMsgTime: 0}, // never messaged
		{JID: "917000000001@s.whatsapp.net", Name: "Harish (old team)", LastMsgTime: 100, IsArchived: true},
		{JID: "919111111111@s.whatsapp.net", Name: "+91 91111 11111", LastMsgTime: 0},
		{JID: "919222222222@s.whatsapp.net", Name: "Mom", LastMsgTime: 200},
	}
	m := New(ch, chats, Options{SidebarWidth: 38})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)

	m, _ = keys(t, m, "/", "h", "a", "r", "i")
	var names []string
	for _, c := range m.visibleChats() {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "Hari Shankar,~ Hari Kumar,Harish (old team)" {
		t.Fatalf("results = %s", got)
	}
	v := stripANSI(m.View())
	if !strings.Contains(v, "start a new chat") || !strings.Contains(v, "ctrl+n/p move") {
		t.Fatalf("contact result not marked:\n%s", v)
	}
	// ctrl+n moves while typing; enter keeps the results to browse
	m, _ = press(t, m, tea.KeyCtrlN)
	m, _ = press(t, m, tea.KeyEnter)
	if m.mode != modeNormal || m.filter != "hari" || m.screen != screenList || m.selectedChat().Name != "~ Hari Kumar" {
		t.Fatalf("after enter: mode %d filter %q screen %d sel %v", m.mode, m.filter, m.screen, m.selectedChat())
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "esc clear") {
		t.Fatalf("no browse hint:\n%s", v)
	}
	// normal keys work on the results: j/k, then enter opens
	m, _ = keys(t, m, "j", "k", "k")
	if m.selectedChat().Name != "Hari Shankar" {
		t.Fatalf("j/k: sel %v", m.selectedChat())
	}
	m, _ = keys(t, m, "j")
	m, cmds := keys(t, m, "enter")
	for _, c := range cmds {
		if c != nil {
			c()
		}
	}
	if c := <-ch; c.Name != "select" || c.Params[0] != "918438018376@s.whatsapp.net" {
		t.Fatalf("opened %+v", c)
	}
	if m.screen != screenChat || m.current.Name != "~ Hari Kumar" || m.filter != "hari" {
		t.Fatalf("after open: screen %d current %v filter %q", m.screen, m.current, m.filter)
	}
	// the results stay in the sidebar: back, next result, open
	m, _ = keys(t, m, "backspace", "j", "enter")
	if m.current.Name != "Harish (old team)" {
		t.Fatalf("second result: %v", m.current)
	}
	// back in the list, esc clears the search
	m, _ = keys(t, m, "backspace", "esc")
	if m.filter != "" || len(m.visibleChats()) != 2 {
		t.Fatalf("esc: filter %q, %d chats", m.filter, len(m.visibleChats()))
	}

	// by phone number
	m, _ = keys(t, m, "/")
	for _, r := range "91111" {
		m, _ = keys(t, m, string(r))
	}
	if v := m.visibleChats(); len(v) != 1 || v[0].JID != "919111111111@s.whatsapp.net" {
		t.Fatalf("number search = %v", v)
	}
	m, _ = press(t, m, tea.KeyEsc)
	if m.filter != "" || m.mode != modeNormal {
		t.Fatal("esc should cancel the search")
	}
}
