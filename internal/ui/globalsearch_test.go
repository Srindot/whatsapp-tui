package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeGlobal struct {
	mu    sync.Mutex
	chats map[string][]messages.Message // chat -> messages, oldest first
	names map[string]string
	loads []string
}

func (f *fakeGlobal) SearchAll(_ context.Context, q string) ([]messages.SearchHit, error) {
	var hits []messages.SearchHit
	for chat, ms := range f.chats {
		for _, m := range ms {
			if strings.Contains(strings.ToLower(m.Text), strings.ToLower(q)) {
				hits = append(hits, messages.SearchHit{Message: m, ChatName: f.names[chat]})
			}
		}
	}
	// newest first
	for i := 0; i < len(hits); i++ {
		for j := i + 1; j < len(hits); j++ {
			if hits[j].Timestamp > hits[i].Timestamp {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}
	return hits, nil
}

func (f *fakeGlobal) LoadAround(_ context.Context, chat, id string) ([]messages.Message, error) {
	f.mu.Lock()
	f.loads = append(f.loads, chat+"|"+id)
	f.mu.Unlock()
	ms := f.chats[chat]
	for i, m := range ms {
		if m.Id == id {
			return ms[i:], nil
		}
	}
	return nil, errors.New("not found")
}

func globalModel(t *testing.T) (Model, *fakeGlobal) {
	t.Helper()
	mk := func(chat string, n int, special map[int]string) []messages.Message {
		var out []messages.Message
		for i := 0; i < n; i++ {
			text := fmt.Sprintf("%s says %d", chat, i)
			if s, ok := special[i]; ok {
				text = s
			}
			out = append(out, messages.Message{Id: fmt.Sprintf("%s-%d", chat, i), ChatId: chat, ContactId: chat,
				ContactShort: "Friend", Timestamp: uint64(1700000000 + i*100), Text: text})
		}
		return out
	}
	fg := &fakeGlobal{
		chats: map[string][]messages.Message{
			"a@s.whatsapp.net": mk("a@s.whatsapp.net", 50, map[int]string{5: "the Wifi password is hunter2", 40: "wifi is down again"}),
			"g@g.us":           mk("g@g.us", 30, map[int]string{29: "who knows the wifi password?"}),
		},
		names: map[string]string{"a@s.whatsapp.net": "Aneesh", "g@g.us": "Hostel"},
	}
	chats := []*messages.Conversation{
		{JID: "a@s.whatsapp.net", Name: "Aneesh", LastMsgTime: 3},
		{JID: "g@g.us", Name: "Hostel", LastMsgTime: 2},
	}
	m := New(make(chan messages.Command, 20), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff,
		GlobalSearcher: fg, Actions: &fakeActions{}})
	// A blinking cursor returns tick commands that drain would wait on.
	m.cmdline.Cursor.SetMode(cursor.CursorStatic)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(Model), fg
}

// typeGlobal types q into the global prompt and applies the searches.
func typeGlobal(t *testing.T, m Model, q string) Model {
	t.Helper()
	for _, r := range q {
		var cmd tea.Cmd
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = drain(t, next.(Model), cmd)
	}
	return m
}

func TestGlobalSearchFindsAcrossChats(t *testing.T) {
	m, _ := globalModel(t)
	m, _ = keys(t, m, "S")
	if m.global == nil || m.mode != modeGlobalSearch {
		t.Fatal("S did not open the global search")
	}
	m = typeGlobal(t, m, "wifi")
	if n := len(m.global.hits); n != 3 {
		t.Fatalf("%d hits, want 3", n)
	}
	// newest first: Hostel (ts +2900) > Aneesh 40 (+4000)? Aneesh 40 is newer.
	if m.global.hits[0].Id != "a@s.whatsapp.net-40" {
		t.Fatalf("first hit %s", m.global.hits[0].Id)
	}
	v := stripANSI(m.View())
	for _, want := range []string{"Search all messages", "3 results", "Aneesh", "Hostel", "wifi password"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	// smartcase narrows the database's case-insensitive match
	m = typeGlobal(t, m, "")
	for i := 0; i < 4; i++ {
		m, _ = press(t, m, tea.KeyBackspace)
		m = drain(t, m, m.runGlobalSearch())
	}
	m = typeGlobal(t, m, "Wifi")
	if n := len(m.global.hits); n != 1 {
		t.Fatalf("smartcase: %d hits, want 1", n)
	}
}

func TestGlobalSearchStaleResultsDropped(t *testing.T) {
	m, _ := globalModel(t)
	m, _ = keys(t, m, "S")
	next, cmdOld := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = next.(Model)
	m = typeGlobal(t, m, "ifi")
	old := cmdOld // result for "w" arrives late
	m = drain(t, m, old)
	for _, h := range m.global.hits {
		if !strings.Contains(strings.ToLower(h.Text), "wifi") {
			t.Fatalf("stale result applied: %q", h.Text)
		}
	}
}

func TestGlobalSearchOpenHitInChat(t *testing.T) {
	m, fg := globalModel(t)
	m, _ = keys(t, m, "S")
	m = typeGlobal(t, m, "wifi")
	m, _ = press(t, m, tea.KeyEnter) // to the results
	if m.global.typing {
		t.Fatal("enter should move to the results")
	}
	m, _ = keys(t, m, "j", "j") // third hit: the old Aneesh message (index 5)
	if m.global.hits[m.global.cursor].Id != "a@s.whatsapp.net-5" {
		t.Fatalf("cursor on %s", m.global.hits[m.global.cursor].Id)
	}
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if m.global != nil || m.current == nil || m.current.JID != "a@s.whatsapp.net" {
		t.Fatalf("hit not opened: global=%v current=%v", m.global != nil, m.current)
	}
	if m.mode != modeVisual || m.msgs[m.sel].Id != "a@s.whatsapp.net-5" {
		t.Fatalf("mode %d, selected %s", m.mode, m.msgs[m.sel].Id)
	}
	if len(fg.loads) != 1 || fg.loads[0] != "a@s.whatsapp.net|a@s.whatsapp.net-5" {
		t.Fatalf("loads = %v", fg.loads)
	}
	// the query carries over: n goes to the next match in this chat
	m, _ = keys(t, m, "N")
	if m.msgs[m.sel].Id != "a@s.whatsapp.net-40" {
		t.Fatalf("N -> %s", m.msgs[m.sel].Id)
	}
	// and actions work on it
	m, _ = keys(t, m, "r")
	if m.replyTo == nil || m.replyTo.Id != "a@s.whatsapp.net-40" {
		t.Fatal("reply target wrong")
	}
}

func TestGlobalSearchCommandAndEsc(t *testing.T) {
	m, _ := globalModel(t)
	m, _ = keys(t, m, ":")
	for _, r := range "search password" {
		m, _ = keys(t, m, string(r))
	}
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if m.global == nil || len(m.global.hits) != 2 {
		t.Fatalf(":search: global=%v", m.global)
	}
	m, _ = press(t, m, tea.KeyEsc)
	if m.global != nil || m.mode != modeNormal {
		t.Fatal("esc did not close the global search")
	}
}

func TestSnippetKeepsMatchVisible(t *testing.T) {
	long := strings.Repeat("blah ", 40) + "the secret word" + strings.Repeat(" more", 20)
	s := snippet(long, "secret", 40)
	if !strings.Contains(s, "secret") || !strings.HasPrefix(s, "…") {
		t.Fatalf("snippet = %q", s)
	}
	if got := snippet("short one", "one", 40); got != "short one" {
		t.Fatalf("short = %q", got)
	}
}
