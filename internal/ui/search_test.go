package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeSearcher struct {
	history []messages.Message // the whole chat
	calls   int
}

func (f *fakeSearcher) SearchHistory(_ context.Context, _ string, q string) ([]messages.Message, int, error) {
	f.calls++
	n, oldest := 0, -1
	for i, m := range f.history {
		if strings.Contains(strings.ToLower(m.Text), strings.ToLower(q)) {
			n++
			if oldest < 0 {
				oldest = i
			}
		}
	}
	if n == 0 {
		return nil, 0, nil
	}
	return f.history[oldest:], n, nil
}

// searchModel opens a chat of 30 messages whose last 10 are loaded.
func searchModel(t *testing.T) (Model, *fakeSearcher) {
	t.Helper()
	var all []messages.Message
	for i := 0; i < 30; i++ {
		text := fmt.Sprintf("message %d", i)
		switch i {
		case 3, 22, 27:
			text = fmt.Sprintf("Pizza tonight? (%d)", i)
		case 25:
			text = "no pizza for me"
		}
		all = append(all, messages.Message{Id: fmt.Sprint("m", i), ChatId: "c@s.whatsapp.net", ContactId: "c@s.whatsapp.net",
			Timestamp: uint64(1700000000 + i*60), Text: text})
	}
	fs := &fakeSearcher{history: all}
	m := New(make(chan messages.Command, 20), []*messages.Conversation{{JID: "c@s.whatsapp.net", Name: "C", LastMsgTime: 1}},
		Options{SidebarWidth: 38, Images: termimg.ModeOff, Searcher: fs, Actions: &fakeActions{}})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	next, _ = m.Update(screenMsg(all[20:]))
	return next.(Model), fs
}

func typeQuery(t *testing.T, m Model, q string) Model {
	t.Helper()
	for _, r := range q {
		m, _ = keys(t, m, string(r))
	}
	return m
}

func TestChatSearchIncremental(t *testing.T) {
	m, _ := searchModel(t)
	m, _ = keys(t, m, "/")
	if m.mode != modeChatSearch {
		t.Fatalf("mode = %d", m.mode)
	}
	m = typeQuery(t, m, "pizza")
	// loaded: m20..m29 -> matches 22, 25, 27 (lowercase query matches any case)
	if got := len(m.search.matches); got != 3 {
		t.Fatalf("%d matches, want 3", got)
	}
	if m.msgs[m.sel].Id != "m27" {
		t.Fatalf("jumped to %s, want the newest match m27", m.msgs[m.sel].Id)
	}
	if !strings.Contains(stripANSI(m.View()), "1/3") && !strings.Contains(stripANSI(m.View()), "3/3") {
		t.Fatalf("match counter missing:\n%s", stripANSI(m.View()))
	}

	// smartcase: an uppercase letter makes it exact
	m, _ = press(t, m, tea.KeyBackspace)
	for i := 0; i < 4; i++ {
		m, _ = press(t, m, tea.KeyBackspace)
	}
	m = typeQuery(t, m, "Pizza")
	if got := len(m.search.matches); got != 2 {
		t.Fatalf("smartcase: %d matches, want 2", got)
	}
}

func TestChatSearchEnterLoadsHistoryAndNavigates(t *testing.T) {
	m, fs := searchModel(t)
	m, _ = keys(t, m, "/")
	m = typeQuery(t, m, "pizza")
	m, cmd := press(t, m, tea.KeyEnter)
	if m.mode != modeVisual {
		t.Fatalf("enter should select the match (visual), mode = %d", m.mode)
	}
	m = drain(t, m, cmd)
	if fs.calls != 1 {
		t.Fatalf("history searched %d times", fs.calls)
	}
	// history adds the old match m3: 4 matches now, messages m3..m29 loaded
	if got := len(m.search.matches); got != 4 || m.msgs[0].Id != "m3" {
		t.Fatalf("%d matches, first loaded %s", got, m.msgs[0].Id)
	}
	if m.msgs[m.sel].Id != "m27" {
		t.Fatalf("selection moved to %s after loading", m.msgs[m.sel].Id)
	}
	ids := func() string { return m.msgs[m.sel].Id }
	m, _ = keys(t, m, "n")
	if ids() != "m25" {
		t.Fatalf("n -> %s, want m25", ids())
	}
	m, _ = keys(t, m, "n", "n")
	if ids() != "m3" {
		t.Fatalf("n n -> %s, want m3", ids())
	}
	m, _ = keys(t, m, "n") // wraps around
	if ids() != "m27" {
		t.Fatalf("wrap -> %s, want m27", ids())
	}
	m, _ = keys(t, m, "N")
	if ids() != "m3" {
		t.Fatalf("N -> %s, want m3", ids())
	}
	// the match is selected, so visual actions apply to it
	m, _ = keys(t, m, "enter")
	if m.replyTo == nil || m.replyTo.Id != "m3" {
		t.Fatal("reply after search should target the match")
	}
}

func TestChatSearchEscRestoresAndClears(t *testing.T) {
	m, _ := searchModel(t)
	before := m.vp.YOffset
	m, _ = keys(t, m, "/")
	m = typeQuery(t, m, "message 21")
	m, _ = press(t, m, tea.KeyEsc)
	if m.search != nil || m.mode != modeNormal || m.vp.YOffset != before {
		t.Fatalf("esc: search=%v mode=%d offset %d->%d", m.search != nil, m.mode, before, m.vp.YOffset)
	}

	// after enter, esc (leave visual) then esc again clears the highlight
	m, _ = keys(t, m, "/")
	m = typeQuery(t, m, "pizza")
	m, _ = press(t, m, tea.KeyEnter)
	m, _ = press(t, m, tea.KeyEsc)
	if m.search == nil || m.mode != modeNormal {
		t.Fatal("first esc should only leave visual mode")
	}
	if !strings.Contains(stripANSI(m.View()), "match") {
		t.Fatal("search status not shown in normal mode")
	}
	m, _ = press(t, m, tea.KeyEsc)
	if m.search != nil {
		t.Fatal("second esc should clear the search")
	}
}

func TestChatSearchNoMatches(t *testing.T) {
	m, _ := searchModel(t)
	m, _ = keys(t, m, "/")
	m = typeQuery(t, m, "zebra")
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if m.search != nil || !m.noticeErr || !strings.Contains(m.notice, "no messages match") {
		t.Fatalf("search=%v notice=%q", m.search != nil, m.notice)
	}
}

func TestHighlight(t *testing.T) {
	m, _ := searchModel(t)
	m.search = &chatSearch{query: "pizza"}
	out := m.highlight("no Pizza or pizza", false, styleBase)
	if stripANSI(out) != "no Pizza or pizza" {
		t.Fatalf("text changed: %q", stripANSI(out))
	}
	m.search.query = ""
	if m.highlight("x", false, styleBase) != styleBase.Render("x") {
		t.Fatal("empty query should not highlight")
	}
}

func TestChatSearchMoveWhileTyping(t *testing.T) {
	m, _ := searchModel(t)
	m, _ = keys(t, m, "/")
	m = typeQuery(t, m, "pizza") // loaded matches: m22, m25, m27; starts on m27
	id := func() string { return m.msgs[m.sel].Id }
	m, _ = press(t, m, tea.KeyCtrlN)
	if id() != "m25" || m.mode != modeChatSearch {
		t.Fatalf("ctrl+n -> %s (mode %d)", id(), m.mode)
	}
	m, _ = press(t, m, tea.KeyUp)
	if id() != "m22" {
		t.Fatalf("up -> %s", id())
	}
	m, _ = press(t, m, tea.KeyCtrlP)
	if id() != "m25" {
		t.Fatalf("ctrl+p -> %s", id())
	}
	if m.cmdline.Value() != "pizza" {
		t.Fatalf("query changed to %q", m.cmdline.Value())
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "2/3") || !strings.Contains(v, "ctrl+n older") {
		t.Fatalf("hint missing:\n%s", v)
	}
	// enter keeps the current match selected
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if id() != "m25" || m.mode != modeVisual {
		t.Fatalf("after enter: %s mode %d", id(), m.mode)
	}
}
