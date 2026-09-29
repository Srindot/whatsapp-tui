package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Forwarder forwards messages; *messages.SessionManager implements it.
type Forwarder interface {
	ForwardMessage(ctx context.Context, msgID string, to []string) error
}

// forwardPicker is the "Forward to…" screen.
type forwardPicker struct {
	msg    messages.Message
	filter string
	cursor int
	offset int
	marked map[string]bool // chosen chats (JID)
	order  []string        // marked JIDs in the order they were picked
}

// forwardTargets is every chat you could forward to: recent chats first,
// then contacts you never messaged, alphabetically; filtered by name.
func (m Model) forwardTargets() []*messages.Conversation {
	q := strings.ToLower(m.fwd.filter)
	var recent, rest []*messages.Conversation
	for _, c := range m.allChats {
		if q != "" && !strings.Contains(strings.ToLower(chatName(c)), q) {
			continue
		}
		if c.LastMsgTime > 0 {
			recent = append(recent, c)
		} else if strings.TrimSpace(c.Name) != "" && !strings.HasPrefix(c.Name, "+") {
			rest = append(rest, c) // skip nameless numbers nobody would search
		}
	}
	sortChats(recent)
	sort.SliceStable(rest, func(i, j int) bool {
		return strings.ToLower(strings.TrimPrefix(chatName(rest[i]), "~ ")) <
			strings.ToLower(strings.TrimPrefix(chatName(rest[j]), "~ "))
	})
	return append(recent, rest...)
}

func (m *Model) openForward(sel messages.Message) tea.Cmd {
	if m.forwarder == nil {
		m.notice, m.noticeErr = "forwarding isn't available", true
		return nil
	}
	m.fwd = &forwardPicker{msg: sel, marked: map[string]bool{}}
	m.picker = false
	m.cmdline.Prompt = "forward to: "
	m.cmdline.SetValue("")
	return m.cmdline.Focus()
}

func (m *Model) closeForward() {
	m.fwd = nil
	m.cmdline.Blur()
	m.cmdline.Prompt = ":"
}

func (m Model) fwdRows() int {
	return max((m.mainHeight()-headerRows-4)/2, 1)
}

func (m Model) handleForward(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.fwd
	targets := m.forwardTargets()
	move := func(d int) {
		f.cursor = min(max(f.cursor+d, 0), max(len(targets)-1, 0))
		if f.cursor < f.offset {
			f.offset = f.cursor
		}
		if rows := m.fwdRows(); f.cursor >= f.offset+rows {
			f.offset = f.cursor - rows + 1
		}
	}
	switch msg.String() {
	case "esc":
		m.closeForward()
		return m, nil
	case "down", "ctrl+n", "ctrl+j":
		move(1)
		return m, nil
	case "up", "ctrl+p", "ctrl+k":
		move(-1)
		return m, nil
	case "ctrl+d":
		move(m.fwdRows() / 2)
		return m, nil
	case "ctrl+u":
		move(-m.fwdRows() / 2)
		return m, nil
	case " ", "tab":
		if f.cursor < len(targets) {
			jid := targets[f.cursor].JID
			if f.marked[jid] {
				delete(f.marked, jid)
				for i, x := range f.order {
					if x == jid {
						f.order = append(f.order[:i], f.order[i+1:]...)
						break
					}
				}
			} else {
				f.marked[jid] = true
				f.order = append(f.order, jid)
			}
			move(1)
		}
		return m, nil
	case "enter":
		to := append([]string(nil), f.order...)
		if len(to) == 0 && f.cursor < len(targets) {
			to = []string{targets[f.cursor].JID}
		}
		if len(to) == 0 {
			return m, nil
		}
		fw, id := m.forwarder, f.msg.Id
		label := "Forwarded to " + m.targetNames(to)
		m.closeForward()
		m.notice, m.noticeErr = "Forwarding…", false
		if m.mode == modeVisual {
			m.exitVisual()
		}
		return m, m.action(label, func(ctx context.Context) (string, error) {
			return "", fw.ForwardMessage(ctx, id, to)
		})
	}
	var cmd tea.Cmd
	m.cmdline, cmd = m.cmdline.Update(msg)
	if v := m.cmdline.Value(); v != f.filter {
		f.filter, f.cursor, f.offset = v, 0, 0
	}
	return m, cmd
}

// targetNames lists chats by name: "Aneesh", "Aneesh and Hostel", "3 chats".
func (m Model) targetNames(jids []string) string {
	name := func(jid string) string {
		for _, c := range m.allChats {
			if c.JID == jid {
				return chatName(c)
			}
		}
		return jid
	}
	switch len(jids) {
	case 1:
		return name(jids[0])
	case 2:
		return name(jids[0]) + " and " + name(jids[1])
	}
	return fmt.Sprintf("%d chats", len(jids))
}

func (m Model) renderForward(width, height int) string {
	f := m.fwd
	targets := m.forwardTargets()
	title := styleTitle.Foreground(pal.Rose).Render(" Forward to…")
	if n := len(f.order); n > 0 {
		title += "  " + styleBadge.Render(fmt.Sprintf("%d selected", n))
	}
	var b strings.Builder
	b.WriteString(m.renderHeader(title, width, true))

	// what is being forwarded
	preview := strings.Join(strings.Fields(prettyTags(f.msg.Text)), " ")
	bar := lipgloss.NewStyle().Foreground(senderColor(f.msg.ContactId)).Render("▎")
	b.WriteString("\n " + bar + senderStyle(f.msg.ContactId).Render(m.senderName(f.msg)) +
		"\n " + bar + styleDim.Render(ansi.Truncate(preview, width-4, "…")) + "\n")

	if len(targets) == 0 {
		b.WriteString("\n" + styleDim.Render("  No chats match."))
	}
	now := time.Now()
	for i := f.offset; i < len(targets) && i < f.offset+m.fwdRows(); i++ {
		c := targets[i]
		sel := i == f.cursor
		fill := paint(lipgloss.NewStyle(), sel)
		marker := fill.Render("  ")
		if sel {
			marker = paint(styleAccent, sel).Render("▌ ")
		}
		check := paint(styleMuted, sel).Render("○ ")
		if f.marked[c.JID] {
			check = paint(styleUnread, sel).Bold(true).Render("● ")
		}
		name := paint(styleName, sel).Render(chatName(c))
		kind := ""
		if isGroup(c.JID) {
			kind = paint(styleMuted, sel).Render("  group")
		}
		when := paint(styleDim, sel).Render(listTime(c.LastMsgTime, now)) + fill.Render(" ")
		b.WriteString("\n" + fitRow(marker+check+m.avatarCells(c, avatarSmallCols, avatarSmallRows, sel)[0]+
			fill.Render(" ")+name+kind, when, width, fill) + "\n")
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(b.String())
}
