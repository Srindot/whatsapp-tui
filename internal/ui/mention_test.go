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

type fakeMentioner struct {
	mu      sync.Mutex
	members []messages.GroupMember
	loads   int
	sent    []string // "text|jid,jid"
}

func (f *fakeMentioner) GroupMembers(context.Context, string) ([]messages.GroupMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	return f.members, nil
}

func (f *fakeMentioner) SendText(_ context.Context, _, text string, mentions []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text+"|"+strings.Join(mentions, ","))
	return nil
}

func mentionModel(t *testing.T, chat string) (Model, *fakeMentioner, chan messages.Command) {
	t.Helper()
	fm := &fakeMentioner{members: []messages.GroupMember{
		{JID: "111@lid", Name: "Arjun"},
		{JID: "919440438863@s.whatsapp.net", Name: "Hari Shankar"},
		{JID: "222@lid", Name: "Priya"},
		{JID: "333@lid", Name: "Hari Kumar"},
	}}
	ch := make(chan messages.Command, 10)
	m := New(ch, []*messages.Conversation{{JID: chat, Name: "Hostel", LastMsgTime: 1}},
		Options{SidebarWidth: 38, Images: termimg.ModeOff, Mentioner: fm, Actions: &fakeActions{}})
	m.compose.Cursor.SetMode(cursor.CursorStatic) // no blink ticks for drain to wait on
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter", "i")
	return m, fm, ch
}

// typeText types s in insert mode, running the member load.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		}
		next, cmd := m.Update(k)
		m = drain(t, next.(Model), cmd)
	}
	return m
}

func TestMentionPickAndSend(t *testing.T) {
	m, fm, _ := mentionModel(t, groupJID)
	m = typeText(t, m, "dinner @ha")
	if m.mention == nil {
		t.Fatal("picker not open")
	}
	var names []string
	for _, mem := range m.mentionMatches() {
		names = append(names, mem.Name)
	}
	if strings.Join(names, ",") != "Hari Shankar,Hari Kumar" {
		t.Fatalf("matches = %v", names)
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "@Hari Shankar") || !strings.Contains(v, "tab pick") {
		t.Fatalf("picker not shown:\n%s", v)
	}
	m, _ = press(t, m, tea.KeyCtrlN) // Hari Kumar
	m, _ = press(t, m, tea.KeyCtrlP) // back to Hari Shankar
	m, _ = press(t, m, tea.KeyTab)
	if got := m.compose.Value(); got != "dinner @Hari Shankar " || m.mention != nil {
		t.Fatalf("after pick: %q picker=%v", got, m.mention != nil)
	}
	m = typeText(t, m, "and @pri")
	m, _ = press(t, m, tea.KeyEnter) // enter picks while the picker is open
	m = typeText(t, m, "at 8")
	m, cmd := press(t, m, tea.KeyEnter)
	drain(t, m, cmd)
	want := "dinner @919440438863 and @222 at 8|919440438863@s.whatsapp.net,222@lid"
	if len(fm.sent) != 1 || fm.sent[0] != want {
		t.Fatalf("sent %v\nwant %s", fm.sent, want)
	}
	if fm.loads != 1 {
		t.Fatalf("members loaded %d times, want once", fm.loads)
	}
}

func TestMentionEscAndDeletedMention(t *testing.T) {
	m, fm, ch := mentionModel(t, groupJID)
	m = typeText(t, m, "email me @")
	m, _ = press(t, m, tea.KeyEsc)
	if m.mention != nil || m.mode != modeInsert {
		t.Fatal("esc should close the picker and stay in insert mode")
	}
	// pick someone, then delete the mention before sending: plain send
	m = typeText(t, m, "x @arj")
	m, _ = press(t, m, tea.KeyTab)
	for i := 0; i < len("@Arjun "); i++ {
		m, _ = press(t, m, tea.KeyBackspace)
	}
	m = typeText(t, m, "ok")
	m, cmd := press(t, m, tea.KeyEnter)
	cmd()
	if c := <-ch; c.Name != "send" || len(fm.sent) != 0 {
		t.Fatalf("expected a plain send, got %+v / %v", c, fm.sent)
	}
}

func TestNoMentionPickerInOneToOne(t *testing.T) {
	m, _, _ := mentionModel(t, "91111@s.whatsapp.net")
	m = typeText(t, m, "hi @ar")
	if m.mention != nil {
		t.Fatal("no mentions in one-to-one chats")
	}
}

func TestMentionsForSend(t *testing.T) {
	text, jids := mentionsForSend("@Hari and @Hari Shankar, not @Priya",
		[]chosenMention{{"Hari", "1@lid"}, {"Hari Shankar", "2@lid"}, {"Arjun", "3@lid"}})
	if text != "@1 and @2, not @Priya" || strings.Join(jids, ",") != "2@lid,1@lid" {
		t.Fatalf("text %q jids %v", text, jids)
	}
}
