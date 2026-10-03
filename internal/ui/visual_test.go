package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/skratchdot/open-golang/open"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type fakeActions struct {
	mu        sync.Mutex
	replies   []string // "chat|text|quotedID"
	reactions []string // "msgID|emoji"
	saved     []string // "msgID|dir"
	mediaPath string
	resent    []string
	edits     []string // "msgID|text|mentions"
	info      messages.ChatInfo
}

func (a *fakeActions) SendReply(_ context.Context, chat, text string, q messages.Message, _ []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.replies = append(a.replies, chat+"|"+text+"|"+q.Id)
	return nil
}
func (a *fakeActions) SendReaction(_ context.Context, m messages.Message, emoji string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reactions = append(a.reactions, m.Id+"|"+emoji)
	return nil
}
func (a *fakeActions) SaveMedia(_ context.Context, id, dir string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.saved = append(a.saved, id+"|"+dir)
	return filepath.Join(dir, "IMG-"+id+".jpg"), nil
}
func (a *fakeActions) MediaPath(context.Context, string) (string, error) {
	if a.mediaPath == "" {
		return "", errors.New("no media")
	}
	return a.mediaPath, nil
}
func (a *fakeActions) ChatInfo(_ context.Context, jid string) (messages.ChatInfo, error) {
	i := a.info
	i.JID = jid
	return i, nil
}
func (a *fakeActions) DirectChat(_ context.Context, sender string) string {
	return strings.Replace(sender, "@lid", "@s.whatsapp.net", 1)
}
func (a *fakeActions) ChatName(context.Context, string) string { return "Priya" }
func (a *fakeActions) ResendMessage(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resent = append(a.resent, id)
	return nil
}

const groupJID = "222@g.us"

func groupMsgs() []messages.Message {
	return []messages.Message{
		{Id: "m1", ChatId: groupJID, ContactId: "91111@s.whatsapp.net", ContactShort: "Arjun", Timestamp: 1700000000, Text: "dinner at 8?"},
		{Id: "m2", ChatId: groupJID, ContactId: "91222@lid", ContactShort: "Priya", Timestamp: 1700000100, Text: "yes!",
			Reactions: []messages.Reaction{{Sender: "a", Emoji: "👍"}, {Sender: "", Emoji: "👍"}, {Sender: "b", Emoji: "❤️"}}},
		{Id: "m3", ChatId: groupJID, FromMe: true, Timestamp: 1700000200, Text: "count me in",
			QuotedID: "m1", QuotedSender: "91111@s.whatsapp.net", QuotedText: "dinner at 8?"},
	}
}

func visualModel(t *testing.T, a *fakeActions, clip fakeClip) Model {
	t.Helper()
	chats := []*messages.Conversation{
		{JID: groupJID, Name: "Hostel", LastMsgTime: 300, Unread: 4},
		{JID: "91111@s.whatsapp.net", Name: "Arjun", LastMsgTime: 200},
	}
	m := New(make(chan messages.Command, 20), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff, Actions: a, Clipboard: clip})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m, _ = keys(t, m, "enter")
	next, _ = m.Update(screenMsg(groupMsgs()))
	return next.(Model)
}

func TestVisualSelectAndMove(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	m, _ = keys(t, m, "v")
	if m.mode != modeVisual || m.sel != 2 {
		t.Fatalf("mode=%d sel=%d, want visual on the newest message", m.mode, m.sel)
	}
	if !strings.Contains(stripANSI(m.View()), "VISUAL") || !strings.Contains(stripANSI(m.View()), "r reply") {
		t.Fatal("visual badge/hint missing")
	}
	m, _ = keys(t, m, "k", "k", "k")
	if m.sel != 0 {
		t.Fatalf("sel = %d after k k k, want 0 (clamped)", m.sel)
	}
	m, _ = keys(t, m, "G")
	if m.sel != 2 {
		t.Fatalf("G: sel = %d", m.sel)
	}
	m, _ = keys(t, m, "esc")
	if m.mode != modeNormal {
		t.Fatal("esc did not leave visual mode")
	}
}

func TestVisualReply(t *testing.T) {
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	m, _ = keys(t, m, "v", "k", "enter")
	if m.mode != modeInsert || m.replyTo == nil || m.replyTo.Id != "m2" {
		t.Fatalf("mode=%d replyTo=%v", m.mode, m.replyTo)
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "Replying to Priya") {
		t.Fatalf("reply bar missing:\n%s", v)
	}
	m, _ = keys(t, m, "s", "u", "r", "e")
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if len(a.replies) != 1 || a.replies[0] != groupJID+"|sure|m2" {
		t.Fatalf("replies = %v", a.replies)
	}
	if m.replyTo != nil {
		t.Fatal("reply not cleared after sending")
	}

	// ctrl+x cancels a reply
	m, _ = keys(t, m, "esc", "v", "enter")
	m, _ = press(t, m, tea.KeyCtrlX)
	if m.replyTo != nil {
		t.Fatal("ctrl+x did not cancel the reply")
	}
}

func TestVisualPrivateReply(t *testing.T) {
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	m, cmd := keys(t, m, "v", "k", "p")
	m = drain(t, m, tea.Batch(cmd...))
	if m.current == nil || m.current.JID != "91222@s.whatsapp.net" {
		t.Fatalf("private reply opened %v", m.current)
	}
	if m.replyTo == nil || m.replyTo.Id != "m2" || m.mode != modeInsert {
		t.Fatal("reply not prepared in the private chat")
	}
	if !strings.Contains(stripANSI(m.View()), "Private reply to Priya") {
		t.Fatal("private reply bar missing")
	}
	m, _ = keys(t, m, "h", "i")
	m, cmd2 := press(t, m, tea.KeyEnter)
	drain(t, m, cmd2)
	if len(a.replies) != 1 || a.replies[0] != "91222@s.whatsapp.net|hi|m2" {
		t.Fatalf("replies = %v", a.replies)
	}
}

func TestVisualReact(t *testing.T) {
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	m, _ = keys(t, m, "v", "r")
	if !m.picker || !strings.Contains(stripANSI(m.View()), "remove") {
		t.Fatal("reaction picker not shown")
	}
	m, cmds := keys(t, m, "2")
	m = drain(t, m, tea.Batch(cmds...))
	m, cmds = keys(t, m, "r", "x")
	m = drain(t, m, tea.Batch(cmds...))
	m, _ = keys(t, m, "r", "🔥")
	m, cmd := press(t, m, tea.KeyEnter)
	drain(t, m, cmd)
	want := []string{"m3|❤️", "m3|", "m3|🔥"}
	if strings.Join(a.reactions, ",") != strings.Join(want, ",") {
		t.Fatalf("reactions = %v, want %v", a.reactions, want)
	}
}

func TestVisualCopyAndDownload(t *testing.T) {
	a := &fakeActions{}
	var got copied
	m := visualModel(t, a, fakeClip{wrote: &got})
	m, cmds := keys(t, m, "v", "k", "y")
	m = drain(t, m, tea.Batch(cmds...))
	if got.text != "yes!" || m.notice != "Copied text" {
		t.Fatalf("copied %q, notice %q", got.text, m.notice)
	}

	m, cmds = keys(t, m, "s") // s saves (d is delete)
	m = drain(t, m, tea.Batch(cmds...))
	if !m.noticeErr || !strings.Contains(m.notice, "no downloadable media") {
		t.Fatalf("text download notice = %q", m.notice)
	}
}

func TestDownloadDirCommand(t *testing.T) {
	dir := t.TempDir()
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	m.msgs[0].Media, m.msgs[0].MediaType = []byte{1}, messages.MediaImage
	m, _ = keys(t, m, ":")
	for _, r := range "download-dir " + dir {
		m, _ = keys(t, m, string(r))
	}
	m, _ = press(t, m, tea.KeyEnter)
	if !strings.Contains(m.notice, "Downloads now go to") {
		t.Fatalf("notice = %q", m.notice)
	}
	m, cmds := keys(t, m, "v", "g", "g", "s")
	drain(t, m, tea.Batch(cmds...))
	if len(a.saved) != 1 || a.saved[0] != "m1|"+dir {
		t.Fatalf("saved = %v", a.saved)
	}
	_ = os.Remove(dir)
}

func TestQuoteAndReactionsRendered(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	v := stripANSI(m.View())
	for _, want := range []string{"▎Arjun", "▎dinner at 8?", "👍 2", "❤️"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in\n%s", want, v)
		}
	}
}

func TestInfoPanel(t *testing.T) {
	a := &fakeActions{info: messages.ChatInfo{Name: "Hostel", IsGroup: true, About: "OBH floor 3 — mess, sports, memes",
		Participants: []string{"Arjun (admin)", "Priya", "You"}, Admins: 1}}
	m := visualModel(t, a, fakeClip{})
	m, cmds := keys(t, m, "K")
	m = drain(t, m, tea.Batch(cmds...))
	v := stripANSI(m.View())
	for _, want := range []string{"Hostel", "Description", "OBH floor 3", "3 members · 1 admins", "Arjun (admin)"} {
		if !strings.Contains(v, want) {
			t.Fatalf("info missing %q:\n%s", want, v)
		}
	}
	m, _ = keys(t, m, "esc")
	if m.info != nil {
		t.Fatal("esc did not close info")
	}
}

func TestUnreadFilterAndBadge(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	m, _ = keys(t, m, "backspace")
	// Hostel was read by opening it; then a message arrives from Arjun
	next, _ := m.Update(chatListMsg{
		{JID: groupJID, Name: "Hostel", LastMsgTime: 300},
		{JID: "91111@s.whatsapp.net", Name: "Arjun", LastMsgTime: 400, Unread: 1},
	})
	m = next.(Model)
	if v := stripANSI(m.View()); !strings.Contains(v, "● 1 unread") || !strings.Contains(v, "┃") && !strings.Contains(v, "▌") {
		t.Fatalf("unread not highlighted:\n%s", v)
	}
	m, _ = keys(t, m, "u")
	if n := len(m.visibleChats()); n != 1 || m.visibleChats()[0].Name != "Arjun" {
		t.Fatalf("unread filter shows %d chats", n)
	}
	m, _ = keys(t, m, "u")
	if len(m.visibleChats()) != 2 {
		t.Fatal("u did not toggle back")
	}
}

func TestNewlineInCompose(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	ch := make(chan messages.Command, 5)
	m.commands = ch
	m, _ = keys(t, m, "i", "a")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = next.(Model)
	m, _ = keys(t, m, "b")
	m, _ = press(t, m, tea.KeyCtrlJ)
	m, _ = keys(t, m, "c")
	if got := m.compose.Value(); got != "a\nb\nc" {
		t.Fatalf("compose = %q", got)
	}
	if m.compose.Height() != 3 {
		t.Fatalf("compose height = %d, want 3", m.compose.Height())
	}
	// all three lines visible, first line at the top
	box := stripANSI(m.renderCompose(80))
	if !strings.Contains(box, "❯ a") || !strings.Contains(box, "b") || !strings.Contains(box, "c") {
		t.Fatalf("compose box scrolled:\n%s", box)
	}
	m, cmd := press(t, m, tea.KeyEnter)
	cmd()
	if c := <-ch; c.Name != "send" || c.Params[1] != "a\nb\nc" {
		t.Fatalf("sent %+v", c)
	}
	if m.compose.Height() != 1 {
		t.Fatal("compose did not shrink after sending")
	}
}

func TestStatusMarksAndRetry(t *testing.T) {
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	msgs := groupMsgs()
	msgs[2].Status = messages.StatusRead
	msgs = append(msgs,
		messages.Message{Id: "m4", ChatId: groupJID, FromMe: true, Timestamp: 1700000300, Text: "delivered one", Status: messages.StatusDelivered},
		messages.Message{Id: "m5", ChatId: groupJID, FromMe: true, Timestamp: 1700000400, Text: "oops", Status: messages.StatusFailed},
	)
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)
	v := stripANSI(m.View())
	for _, want := range []string{"■■", "✕", "not sent"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	// read and delivered have the same shape and differ in colour
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	if statusMark(messages.StatusRead) == statusMark(messages.StatusDelivered) {
		t.Fatal("read and delivered marks are identical")
	}
	if strings.Contains(v, "hi bob") {
		t.Fatal("unexpected")
	}

	// R retries only a failed message
	m, _ = keys(t, m, "v", "k", "R")
	if len(a.resent) != 0 || !m.noticeErr {
		t.Fatal("R on a delivered message should refuse")
	}
	m, cmds := keys(t, m, "j", "R")
	drain(t, m, tea.Batch(cmds...))
	if len(a.resent) != 1 || a.resent[0] != "m5" {
		t.Fatalf("resent = %v", a.resent)
	}
}

func TestCtrlXCancelsReplyInAnyMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string // after starting a reply (which leaves you in insert mode)
		mode mode
	}{
		{"insert", nil, modeInsert},
		{"normal", []string{"esc"}, modeNormal},
		{"visual", []string{"esc", "v"}, modeVisual},
		{"sidebar", []string{"esc", "h"}, modeNormal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := visualModel(t, &fakeActions{}, fakeClip{})
			m, _ = keys(t, m, "v", "k", "enter")
			m, _ = keys(t, m, tc.keys...)
			if m.replyTo == nil || m.mode != tc.mode {
				t.Fatalf("setup: replyTo=%v mode=%d", m.replyTo != nil, m.mode)
			}
			vpBefore := m.vp.Height
			m, _ = press(t, m, tea.KeyCtrlX)
			if m.replyTo != nil {
				t.Fatal("ctrl+x did not cancel the reply")
			}
			if m.mode != tc.mode {
				t.Fatalf("mode changed to %d", m.mode)
			}
			if m.vp.Height != vpBefore+2 {
				t.Fatalf("reply bar space not given back: %d -> %d", vpBefore, m.vp.Height)
			}
			if strings.Contains(stripANSI(m.View()), "Replying to") {
				t.Fatal("reply bar still shown")
			}
		})
	}
}

type fakePrivacy struct {
	off   bool
	calls int
	self  string
}

func (p *fakePrivacy) ReadReceiptsOff(context.Context) (bool, error) { p.calls++; return p.off, nil }
func (p *fakePrivacy) IsSelfChat(jid string) bool                    { return jid == p.self }

func TestReadReceiptsOffNote(t *testing.T) {
	p := &fakePrivacy{off: true, self: "91999@s.whatsapp.net"}
	chats := []*messages.Conversation{
		{JID: "91111@s.whatsapp.net", Name: "Arjun", LastMsgTime: 3},
		{JID: groupJID, Name: "Hostel", LastMsgTime: 2},
		{JID: "91999@s.whatsapp.net", Name: "Me (Myself & I)", LastMsgTime: 1},
	}
	m := New(make(chan messages.Command, 20), chats, Options{SidebarWidth: 38, Images: termimg.ModeOff, Privacy: p})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m = next.(Model)
	next, cmd := m.Update(statusMsg{Connected: true})
	m = drain(t, next.(Model), cmd)
	next, cmd = m.Update(statusMsg{Connected: true}) // checked once per session
	m = drain(t, next.(Model), cmd)
	if !m.readReceiptsOff || p.calls != 1 {
		t.Fatalf("off=%v calls=%d", m.readReceiptsOff, p.calls)
	}
	note := "your read receipts are off"
	m, _ = keys(t, m, "enter") // Arjun: one-to-one, note shown
	if !strings.Contains(stripANSI(m.View()), note) {
		t.Fatal("note missing in a one-to-one chat")
	}
	m, _ = keys(t, m, "h", "j", "enter") // Hostel: groups still get read receipts
	if strings.Contains(stripANSI(m.View()), note) {
		t.Fatal("note shown in a group")
	}
	m, _ = keys(t, m, "h", "j", "enter") // your own chat: everything is read
	if strings.Contains(stripANSI(m.View()), note) {
		t.Fatal("note shown in your own chat")
	}
}

func TestLinksInChatAndOpenWithO(t *testing.T) {
	var opened []string
	openURL = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURL = open.Start }()

	url := "https://www.linkedin.com/jobs/view/4470560160/?refId=abcdefghijkl&trackingId=xyz"
	m := visualModel(t, &fakeActions{}, fakeClip{})
	msgs := groupMsgs()
	msgs = append(msgs, messages.Message{Id: "m9", ChatId: groupJID, ContactId: "91111@s.whatsapp.net",
		ContactShort: "Arjun", Timestamp: 1700000500, Text: "job: " + url + " and www.iiit.ac.in"})
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)

	view := m.View()
	pieces := 0
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 120 {
			t.Fatal("a line overflows the screen")
		}
		for _, mm := range osc8.FindAllStringSubmatch(line, -1) {
			if mm[1] == url {
				pieces++
			}
		}
	}
	if pieces < 2 {
		t.Fatalf("the wrapped link is marked on %d lines, want every line it spans", pieces)
	}

	m, cmds := keys(t, m, "v", "o")
	m = drain(t, m, tea.Batch(cmds...))
	if len(opened) != 1 || opened[0] != url || m.notice != "Opened the first of 2 links" {
		t.Fatalf("opened %v, notice %q", opened, m.notice)
	}
}

func (a *fakeActions) EditMessage(_ context.Context, m messages.Message, text string, mentions []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.edits = append(a.edits, m.Id+"|"+text+"|"+strings.Join(mentions, ","))
	return nil
}
