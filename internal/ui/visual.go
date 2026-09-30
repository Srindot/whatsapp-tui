package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/skratchdot/open-golang/open"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// Actions are the message operations of visual mode;
// *messages.SessionManager implements it.
type Actions interface {
	SendReply(ctx context.Context, chat, text string, quoted messages.Message, mentions []string) error
	SendReaction(ctx context.Context, m messages.Message, emoji string) error
	SaveMedia(ctx context.Context, msgID, dir string) (string, error)
	MediaPath(ctx context.Context, msgID string) (string, error)
	ChatInfo(ctx context.Context, jid string) (messages.ChatInfo, error)
	DirectChat(ctx context.Context, sender string) string
	ChatName(ctx context.Context, jid string) string
	ResendMessage(ctx context.Context, msgID string) error
}

// quickReactions are offered by number in the reaction picker.
var quickReactions = []string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

// actionDoneMsg reports the result of a visual-mode action.
type actionDoneMsg struct {
	ok  string // notice on success
	err error
}

// privateChatMsg opens a one-to-one chat for a private reply.
type privateChatMsg struct {
	jid, name string
	quoted    messages.Message
}

func (m Model) action(ok string, f func(ctx context.Context) (string, error)) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		text, err := f(ctx)
		if text != "" {
			ok = text
		}
		return actionDoneMsg{ok: ok, err: err}
	}
}

// ---------- selection ----------

func (m *Model) enterVisual() {
	if len(m.msgs) == 0 {
		m.notice, m.noticeErr = "no messages to select", true
		return
	}
	m.mode = modeVisual
	m.sel = len(m.msgs) - 1
	m.refreshMessages(false)
	m.scrollToSelection()
}

func (m *Model) exitVisual() {
	m.mode = modeNormal
	m.picker = false
	m.refreshMessages(false)
}

func (m *Model) moveSelection(delta int) {
	m.sel = min(max(m.sel+delta, 0), len(m.msgs)-1)
	m.refreshMessages(false)
	m.scrollToSelection()
}

// scrollToSelection keeps the selected message on screen.
func (m *Model) scrollToSelection() {
	for _, sp := range m.msgSpans {
		if sp.idx != m.sel {
			continue
		}
		switch {
		case sp.end-sp.start+1 >= m.vp.Height || sp.start < m.vp.YOffset:
			m.vp.SetYOffset(sp.start)
		case sp.end >= m.vp.YOffset+m.vp.Height:
			m.vp.SetYOffset(sp.end - m.vp.Height + 1)
		}
		return
	}
}

func (m Model) selected() (messages.Message, bool) {
	if m.sel >= 0 && m.sel < len(m.msgs) {
		return m.msgs[m.sel], true
	}
	return messages.Message{}, false
}

// messageText is the text part of a message, without the media tag.
func messageText(msg messages.Message) string {
	_, rest := splitTag(msg.Text)
	return rest
}

// senderName is how a message's author is shown.
func (m Model) senderName(msg messages.Message) string {
	if msg.FromMe {
		return "You"
	}
	if msg.ContactShort != "" {
		return msg.ContactShort
	}
	if msg.ContactName != "" {
		return msg.ContactName
	}
	return chatName(m.current)
}

func (m Model) handleVisual(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.picker {
		return m.handlePicker(msg)
	}
	if m.pendingG {
		m.pendingG = false
		if key == "g" {
			m.sel = 0
			m.refreshMessages(false)
			m.scrollToSelection()
			return m, nil
		}
	}
	sel, ok := m.selected()
	if !ok {
		m.exitVisual()
		return m, nil
	}
	switch key {
	case "esc", "v", "q":
		m.exitVisual()
	case "/":
		return m, m.startSearch()
	case "@":
		m.nextMention()
	case "n":
		m.nextMatch(-1)
	case "N":
		m.nextMatch(1)
	case "j", "down":
		m.moveSelection(1)
	case "k", "up":
		m.moveSelection(-1)
	case "ctrl+d":
		m.moveSelection(5)
	case "ctrl+u":
		m.moveSelection(-5)
	case "G":
		m.moveSelection(len(m.msgs))
	case "g":
		m.pendingG = true
	case "r", "enter":
		return m.startReply(sel)
	case "p":
		if m.current == nil || !isGroup(m.current.JID) || sel.FromMe {
			return m.startReply(sel) // in a one-to-one chat it's the same
		}
		return m, m.openPrivate(sel)
	case "e":
		m.picker = true
		m.cmdline.Prompt = "react: "
		m.cmdline.SetValue("")
		return m, m.cmdline.Focus()
	case "f":
		return m, m.openForward(sel)
	case "y":
		return m, m.copyMessage(sel)
	case "s":
		return m, m.downloadMessage(sel)
	case "d":
		m.askDeleteMessage(sel)
	case "o":
		return m, m.openMessage(sel)
	case " ", "space":
		return m, m.viewMedia(sel)
	case "R":
		if !sel.FromMe || sel.Status != messages.StatusFailed {
			m.notice, m.noticeErr = "only messages that failed to send can be retried", true
			return m, nil
		}
		if m.actions == nil {
			return m, nil
		}
		a := m.actions
		m.exitVisual()
		return m, m.action("Sent", func(ctx context.Context) (string, error) {
			return "", a.ResendMessage(ctx, sel.Id)
		})
	}
	return m, nil
}

// ---------- reply ----------

func (m Model) startReply(sel messages.Message) (tea.Model, tea.Cmd) {
	m.replyTo = &sel
	m.mode = modeInsert
	m.picker = false
	m.refreshMessages(false)
	m.resize()
	m.vp.GotoBottom()
	return m, m.compose.Focus()
}

func (m Model) openPrivate(sel messages.Message) tea.Cmd {
	if m.actions == nil {
		return nil
	}
	a := m.actions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		jid := a.DirectChat(ctx, sel.ContactId)
		return privateChatMsg{jid: jid, name: a.ChatName(ctx, jid), quoted: sel}
	}
}

// openPrivateChat switches to the sender's chat with the reply prepared.
func (m Model) openPrivateChat(msg privateChatMsg) (tea.Model, tea.Cmd) {
	var conv *messages.Conversation
	for _, c := range m.chats {
		if c.JID == msg.jid {
			conv = c
			break
		}
	}
	if conv == nil { // never chatted: a chat that isn't in the list yet
		conv = &messages.Conversation{JID: msg.jid, Name: msg.name}
	}
	m.mode = modeNormal
	cmd := m.openChat(conv)
	next, focus := m.startReply(msg.quoted)
	return next, tea.Batch(cmd, focus)
}

func (m Model) replyRows() int {
	if m.replyTo == nil {
		return 0
	}
	return 2
}

// renderReplyBar shows what the message being written replies to.
func (m Model) renderReplyBar(width int) string {
	q := *m.replyTo
	who := m.senderName(q)
	title := "↩ Replying to " + who
	if m.current != nil && q.ChatId != m.current.JID {
		title = "↩ Private reply to " + who + " (from the group)"
	}
	text := strings.ReplaceAll(prettyTags(q.Text), "\n", " ")
	bar := lipgloss.NewStyle().Foreground(senderColor(q.ContactId)).Render("▎")
	lines := []string{
		bar + senderStyle(q.ContactId).Render(ansi.Truncate(title, width-24, "…")) + styleMuted.Render("   ctrl+x cancel"),
		bar + styleDim.Render(ansi.Truncate(text, width-3, "…")),
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).PaddingLeft(1).Render(strings.Join(lines, "\n"))
}

func (m *Model) cancelReply() {
	m.replyTo = nil
	m.resize()
}

// ---------- reactions ----------

func (m Model) handlePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	sel, _ := m.selected()
	send := func(emoji string) (tea.Model, tea.Cmd) {
		m.picker = false
		m.cmdline.Blur()
		m.cmdline.Prompt = ":"
		if m.actions == nil {
			return m, nil
		}
		a := m.actions
		label := "Reacted " + emoji
		if emoji == "" {
			label = "Reaction removed"
		}
		return m, m.action(label, func(ctx context.Context) (string, error) {
			return "", a.SendReaction(ctx, sel, emoji)
		})
	}
	switch {
	case key == "esc":
		m.picker = false
		m.cmdline.Blur()
		m.cmdline.Prompt = ":"
		return m, nil
	case m.cmdline.Value() == "" && len(key) == 1 && key >= "1" && key <= "6":
		return send(quickReactions[key[0]-'1'])
	case m.cmdline.Value() == "" && key == "x":
		return send("")
	case key == "enter":
		if v := strings.TrimSpace(m.cmdline.Value()); v != "" {
			return send(v)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.cmdline, cmd = m.cmdline.Update(msg)
	return m, cmd
}

func (m Model) renderPicker() string {
	var b strings.Builder
	for i, e := range quickReactions {
		fmt.Fprintf(&b, "%s %s  ", styleFilter.Render(fmt.Sprint(i+1)), e)
	}
	b.WriteString(styleFilter.Render("x") + styleDim.Render(" remove  ·  or type an emoji: "))
	return b.String() + m.cmdline.View()
}

// ---------- copy / download / open ----------

func (m Model) copyMessage(sel messages.Message) tea.Cmd {
	text := messageText(sel)
	meta, hasMedia := sel.MediaMeta()
	image := hasMedia && (meta.Type == messages.MediaImage || meta.Type == messages.MediaSticker)
	if !image {
		if text == "" {
			return func() tea.Msg { return actionDoneMsg{err: errors.New("nothing to copy")} }
		}
		clip := m.clip
		return func() tea.Msg { return actionDoneMsg{ok: "Copied text", err: clip.WriteText(text)} }
	}
	a, clip := m.actions, m.clip
	return m.action("Copied image", func(ctx context.Context) (string, error) {
		if a == nil {
			return "", errors.New("copying images isn't available")
		}
		path, err := a.MediaPath(ctx, sel.Id)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		// Most apps paste PNG; convert (first frame for animated stickers).
		frames, err := termimg.DecodeFrames(data, 4096)
		if err != nil {
			return "", err
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, frames[0].Img); err != nil {
			return "", err
		}
		if text != "" {
			return "Copied image and text", clip.WriteImage(buf.Bytes(), "image/png", text)
		}
		return "", clip.WriteImage(buf.Bytes(), "image/png", "")
	})
}

func (m Model) downloadMessage(sel messages.Message) tea.Cmd {
	if len(sel.Media) == 0 {
		return func() tea.Msg {
			return actionDoneMsg{err: errors.New("this message has no downloadable media")}
		}
	}
	a, dir := m.actions, downloadDir()
	return m.action("", func(ctx context.Context) (string, error) {
		if a == nil {
			return "", errors.New("downloads aren't available")
		}
		path, err := a.SaveMedia(ctx, sel.Id, dir)
		if err != nil {
			return "", err
		}
		return "Saved " + tildePath(path), nil
	})
}

// openURL is how links are opened (swappable for tests).
var openURL = open.Start

func (m Model) openMessage(sel messages.Message) tea.Cmd {
	if len(sel.Media) == 0 {
		// no media: open the message's first link instead
		loc := linkRe.FindStringIndex(sel.Text)
		if loc == nil {
			return func() tea.Msg { return actionDoneMsg{err: errors.New("nothing to open")} }
		}
		url := sel.Text[loc[0]:loc[1]]
		if !strings.Contains(strings.ToLower(url), "://") {
			url = "https://" + url
		}
		label := "Opened " + url
		if n := len(linkRe.FindAllStringIndex(sel.Text, -1)); n > 1 {
			label = fmt.Sprintf("Opened the first of %d links", n)
		}
		return func() tea.Msg { return actionDoneMsg{ok: label, err: openURL(url)} }
	}
	a := m.actions
	return m.action("Opened in the default app", func(ctx context.Context) (string, error) {
		if a == nil {
			return "", errors.New("opening media isn't available")
		}
		// Save with a proper extension so the viewer knows the type.
		path, err := a.SaveMedia(ctx, sel.Id, os.TempDir())
		if err != nil {
			return "", err
		}
		return "", open.Start(path)
	})
}

func downloadDir() string {
	if d := config.Config.General.DownloadPath; d != "" {
		return config.ExpandPath(d)
	}
	return config.ExpandPath("~/Downloads")
}

func tildePath(p string) string {
	home := strings.TrimSuffix(config.GetHomeDir(), "/")
	if home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
