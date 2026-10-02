package ui

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Mentioner lists group members and sends messages that @mention them;
// *messages.SessionManager implements it.
type Mentioner interface {
	GroupMembers(ctx context.Context, chat string) ([]messages.GroupMember, error)
	SendText(ctx context.Context, chat, text string, mentions []string) error
}

// mentionRows is how many members the picker shows at once.
const mentionRows = 5

// mentionPicker is open while you type "@name" in a group.
type mentionPicker struct {
	query  string
	cursor int
}

// chosenMention is a member picked into the message being written.
type chosenMention struct {
	name, jid string
}

type membersMsg struct {
	chat    string
	members []messages.GroupMember
	err     error
}

// trailingMention matches an "@partial name" being typed at the end.
var trailingMention = regexp.MustCompile(`(?:^|\s)@([^\s@]*(?: [^\s@]+)?)$`)

// updateMentions opens, narrows or closes the picker after an edit.
func (m *Model) updateMentions() tea.Cmd {
	if m.mentioner == nil || m.current == nil || !isGroup(m.current.JID) {
		m.mention = nil
		return nil
	}
	match := trailingMention.FindStringSubmatch(m.compose.Value())
	if match == nil {
		m.mention = nil
		return nil
	}
	q := match[1]
	// "@Name " with a finished name (the space after a pick) closes it
	if m.mention == nil && strings.Contains(q, " ") {
		return nil
	}
	if m.mention == nil {
		m.mention = &mentionPicker{}
	}
	if m.mention.query != q {
		m.mention.query, m.mention.cursor = q, 0
	}
	if len(m.mentionMatches()) == 0 && strings.Contains(q, " ") {
		m.mention = nil // typed past any name: it's just text
		return nil
	}
	if _, ok := m.members[m.current.JID]; !ok {
		m.members[m.current.JID] = nil // loading
		mn, chat := m.mentioner, m.current.JID
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			ms, err := mn.GroupMembers(ctx, chat)
			return membersMsg{chat: chat, members: ms, err: err}
		}
	}
	return nil
}

// mentionMatches filters the group's members by the typed query: names
// starting with it first, then names containing it.
func (m Model) mentionMatches() []messages.GroupMember {
	if m.mention == nil || m.current == nil {
		return nil
	}
	q := strings.ToLower(m.mention.query)
	var starts, contains []messages.GroupMember
	for _, mem := range m.members[m.current.JID] {
		name := strings.ToLower(mem.Name)
		switch {
		case strings.HasPrefix(name, q):
			starts = append(starts, mem)
		case strings.Contains(name, q):
			contains = append(contains, mem)
		}
	}
	return append(starts, contains...)
}

// handleMentionKey handles keys while the picker is open; ok is false when
// the key is for the input box instead.
func (m *Model) handleMentionKey(key string) (ok bool) {
	matches := m.mentionMatches()
	switch key {
	case "esc":
		m.mention = nil
		return true
	case "ctrl+n", "down":
		if len(matches) > 0 {
			m.mention.cursor = (m.mention.cursor + 1) % len(matches)
		}
		return true
	case "ctrl+p", "up":
		if len(matches) > 0 {
			m.mention.cursor = (m.mention.cursor - 1 + len(matches)) % len(matches)
		}
		return true
	case "tab", "enter":
		if len(matches) == 0 {
			return key == "tab" // enter with nothing to pick sends
		}
		mem := matches[min(m.mention.cursor, len(matches)-1)]
		v := m.compose.Value()
		v = v[:len(v)-len("@"+m.mention.query)] + "@" + mem.Name + " "
		m.compose.SetValue(v)
		m.chosen = append(m.chosen, chosenMention{name: mem.Name, jid: mem.JID})
		m.mention = nil
		m.fitCompose()
		return true
	}
	return false
}

// mentionsForSend turns picked "@Name"s still in text into WhatsApp
// mentions: the text gets "@<number>" and the JIDs are returned.
func mentionsForSend(text string, chosen []chosenMention) (string, []string) {
	// longest names first, so "@Hari Shankar" wins over "@Hari"
	sorted := append([]chosenMention(nil), chosen...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i].name) > len(sorted[j].name) })
	var jids []string
	seen := map[string]bool{}
	for _, c := range sorted {
		tag := "@" + c.name
		if !strings.Contains(text, tag) {
			continue // deleted after picking
		}
		user := strings.Split(c.jid, "@")[0]
		text = strings.ReplaceAll(text, tag, "@"+user)
		if !seen[c.jid] {
			seen[c.jid] = true
			jids = append(jids, c.jid)
		}
	}
	return text, jids
}

func (m Model) mentionPickerRows() int {
	if m.mention == nil {
		return 0
	}
	return min(max(len(m.mentionMatches()), 1), mentionRows) + 1
}

func (m Model) renderMentionPicker(width int) string {
	matches := m.mentionMatches()
	title := styleDim.Render(" mention · ctrl+n/p move · tab pick · esc close")
	var lines []string
	lines = append(lines, title)
	switch {
	case m.members[m.current.JID] == nil:
		lines = append(lines, styleMuted.Render("  loading members…"))
	case len(matches) == 0:
		lines = append(lines, styleMuted.Render("  no member matches @"+m.mention.query))
	}
	start := max(0, min(m.mention.cursor-mentionRows+1, len(matches)-mentionRows))
	for i := start; i < len(matches) && i < start+mentionRows; i++ {
		mem := matches[i]
		sel := i == m.mention.cursor
		fill := paint(lipgloss.NewStyle(), sel)
		marker := fill.Render("  ")
		if sel {
			marker = paint(styleAccent, sel).Render("▌ ")
		}
		name := paint(senderStyle(mem.JID), sel).Render("@" + mem.Name)
		if mem.JID == messages.MentionAll {
			name = paint(styleAccent.Bold(true), sel).Render("@all") + paint(styleMuted, sel).Render("  everyone in the group")
		}
		lines = append(lines, fitRow(marker+name, fill.Render(" "), width, fill))
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).MaxHeight(m.mentionPickerRows()).
		Render(strings.Join(lines, "\n"))
}
