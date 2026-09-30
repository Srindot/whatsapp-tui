package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Deleter deletes messages and chats; *messages.SessionManager implements it.
type Deleter interface {
	DeleteForMe(ctx context.Context, m messages.Message) error
	DeleteForEveryone(ctx context.Context, m messages.Message) error
	DeleteChat(ctx context.Context, chat string) error
}

// confirmDelete asks before deleting (d, then enter).
type confirmDelete struct {
	msg  *messages.Message      // deleting a message
	chat *messages.Conversation // or a whole chat
}

type chatDeletedMsg struct {
	jid string
	err error
}

func (m *Model) askDeleteMessage(sel messages.Message) {
	if m.deleter == nil {
		m.notice, m.noticeErr = "deleting isn't available", true
		return
	}
	m.confirm = &confirmDelete{msg: &sel}
}

func (m *Model) askDeleteChat(c *messages.Conversation) {
	if c == nil {
		return
	}
	if m.deleter == nil {
		m.notice, m.noticeErr = "deleting isn't available", true
		return
	}
	m.confirm = &confirmDelete{chat: c}
}

func (m Model) handleConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c, d := m.confirm, m.deleter
	m.confirm = nil
	run := func(label string, f func(ctx context.Context) error) tea.Cmd {
		return m.action(label, func(ctx context.Context) (string, error) { return "", f(ctx) })
	}
	switch key := msg.String(); {
	case c.msg != nil && key == "enter":
		sel := *c.msg
		return m, run("Deleted for you", func(ctx context.Context) error { return d.DeleteForMe(ctx, sel) })
	case c.msg != nil && key == "e" && messages.CanDeleteForEveryone(*c.msg):
		sel := *c.msg
		return m, run("Deleted for everyone", func(ctx context.Context) error { return d.DeleteForEveryone(ctx, sel) })
	case c.chat != nil && key == "enter":
		jid := c.chat.JID
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return chatDeletedMsg{jid: jid, err: d.DeleteChat(ctx, jid)}
		}
	}
	m.notice, m.noticeErr = "Not deleted", false // any other key cancels
	return m, nil
}

func (m Model) applyChatDeleted(r chatDeletedMsg) (tea.Model, tea.Cmd) {
	if r.err != nil {
		m.notice, m.noticeErr = r.err.Error(), true
		return m, nil
	}
	if m.current != nil && m.current.JID == r.jid {
		m.back()
		m.current, m.msgs = nil, nil
	}
	m.notice, m.noticeErr = "Chat deleted", false
	return m, nil
}

// renderConfirm is the question on the command line.
func (m Model) renderConfirm() string {
	c := m.confirm
	if c.chat != nil {
		return styleErr.Render(fmt.Sprintf("Delete chat “%s” and its messages on all your devices?", chatName(c.chat))) +
			styleDim.Render("  enter delete · esc cancel")
	}
	q := styleErr.Render("Delete this message?") + styleDim.Render("  enter for me")
	if messages.CanDeleteForEveryone(*c.msg) {
		q += styleDim.Render(" · e for everyone")
	}
	return q + styleDim.Render(" · esc cancel")
}
