package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Avatar sizes in cells: the two-line list entry, and one-line rows.
const (
	avatarBigCols, avatarBigRows     = 4, 2
	avatarSmallCols, avatarSmallRows = 2, 1
)

func (m Model) renderHeader(title string, width int, focused bool) string {
	sep := lipgloss.NewStyle().Foreground(colorBorder)
	if focused {
		sep = sep.Foreground(colorBorderFocus)
	}
	return ansi.Truncate(title, width, "…") + "\n" + sep.Render(strings.Repeat("─", width))
}

// listTitle shows "Chats" and "Archived" as tabs (the active one
// highlighted) and how many chats are unread.
func (m Model) listTitle(width int) string {
	inbox, archived, unread := m.chatCounts()
	tab := func(label string, n int, active bool) string {
		if active {
			return styleTitle.Foreground(pal.Rose).Render(label) + styleDim.Render(fmt.Sprintf(" %d", n))
		}
		return styleMuted.Render(fmt.Sprintf("%s %d", label, n))
	}
	title := " " + tab("Chats", inbox, !m.archive)
	if archived > 0 || m.archive {
		title += styleMuted.Render("  ·  ") + tab("Archived", archived, m.archive)
	}
	switch {
	case m.unreadOnly:
		title += "  " + styleBadge.Render(fmt.Sprintf("unread only · %d", unread))
	case unread > 0:
		title += "  " + styleUnread.Bold(true).Render(fmt.Sprintf("● %d unread", unread))
	}
	if m.filter != "" {
		title += styleFilter.Render("  /" + m.filter)
		if m.mode == modeFilter {
			title += styleMuted.Render("  ctrl+n/p move · enter browse")
		} else {
			title += styleMuted.Render("  esc clear")
		}
	}
	return title
}

// avatarCells returns an avatar's rows (cols wide each): the profile picture
// when loaded, otherwise a coloured tile with the chat's initial.
func (m Model) avatarCells(c *messages.Conversation, cols, rows int, sel bool) []string {
	if e := m.img.get(imgKey{imgAvatar, c.JID, cols, rows}); e != nil && e.state == imgReady {
		lines := strings.Split(e.text, "\n")
		if sel { // selection background shows through the circle's corners
			for i, l := range lines {
				lines[i] = styleSelected.Render(l)
			}
		}
		return lines
	}
	if rows == 1 {
		return []string{paint(senderStyle(c.JID), sel).Render(ansi.Truncate(initial(chatName(c))+"  ", cols, ""))}
	}
	tile := lipgloss.NewStyle().Background(senderColor(c.JID)).Foreground(colorBadgeFg).Bold(true)
	out := make([]string, rows)
	for r := range out {
		text := strings.Repeat(" ", cols)
		if r == (rows-1)/2 {
			text = lipgloss.PlaceHorizontal(cols, lipgloss.Center, initial(chatName(c)))
		}
		out[r] = tile.Render(text)
	}
	return out
}

func previewText(c *messages.Conversation) string {
	return plainText(prettyTags(strings.ReplaceAll(c.Preview, "\n", " ")))
}

// renderEntry draws one two-line chat entry. Unread chats get a foam bar
// down the left edge, a bold name, a bright preview and a count badge.
func (m Model) renderEntry(c *messages.Conversation, width int, sel, open bool, now time.Time) string {
	fill := paint(lipgloss.NewStyle(), sel)
	unread := c.Unread > 0

	marker := fill.Render("  ")
	switch {
	case sel:
		marker = paint(styleAccent, sel).Render("▌ ")
	case unread:
		marker = styleUnread.Render("┃ ")
	case open:
		marker = styleAccent.Render("▎ ")
	}
	av := m.avatarCells(c, avatarBigCols, avatarBigRows, sel)

	nameStyle, timeStyle, previewStyle := styleName, styleDim, styleDim
	if unread {
		nameStyle = styleNameBold.Foreground(pal.Text)
		timeStyle = styleUnread.Bold(true)
		previewStyle = styleBase
	}
	if open {
		nameStyle = nameStyle.Bold(true).Foreground(pal.Rose)
	}
	left1 := marker + av[0] + fill.Render(" ") + paint(nameStyle, sel).Render(chatName(c))
	right1 := paint(timeStyle, sel).Render(listTime(c.LastMsgTime, now)) + fill.Render(" ")
	if c.IsPinned {
		right1 = paint(styleMuted, sel).Render("📌 ") + right1
	}

	preview := paint(previewStyle, sel).Render(previewText(c))
	switch {
	case c.LastMsgTime == 0:
		preview = paint(styleMuted, sel).Italic(true).Render("start a new chat")
	case c.IsArchived && !m.archive:
		preview = paint(styleMuted, sel).Render("📦 ") + preview // archived chat found by the filter
	}
	left2 := marker + av[1] + fill.Render(" ") + preview
	right2 := fill.Render(" ")
	if unread {
		right2 = styleBadge.Render(fmt.Sprint(c.Unread)) + fill.Render(" ")
	}
	if c.Mentioned && unread {
		right2 = styleMentionBadge.Render("@") + fill.Render(" ") + right2
	}
	return fitRow(left1, right1, width, fill) + "\n" + fitRow(left2, right2, width, fill)
}

// renderList draws the chat list: full screen at start, as the sidebar
// next to an open chat. Both use the same two-line entries.
func (m Model) renderList(width, height int, focused bool) string {
	var b strings.Builder
	b.WriteString(m.renderHeader(m.listTitle(width), width, focused))
	if m.listLen() == 0 {
		b.WriteString("\n\n" + styleDim.Render(emptyListText(m)))
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(b.String())
	}
	now := time.Now()
	rows := m.listRows()
	// A thin divider under the text (not the avatar) separates entries,
	// like WhatsApp's list.
	indent := 2 + avatarBigCols + 1
	divider := strings.Repeat(" ", indent) +
		lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", max(width-indent-1, 0)))
	for i := m.listOffset; i < m.listLen() && i < m.listOffset+rows; i++ {
		c, archiveRow := m.itemAt(i)
		sel := i == m.cursor && focused
		if archiveRow {
			b.WriteString("\n" + m.renderArchiveRow(width, sel) + "\n" + divider)
			continue
		}
		open := m.screen == screenChat && m.current != nil && c.JID == m.current.JID
		b.WriteString("\n" + m.renderEntry(c, width, sel, open, now) + "\n" + divider)
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(b.String())
}

// renderFullList renders the start screen.
func (m Model) renderFullList(width, height int) string { return m.renderList(width, height, true) }

// renderSidebar renders the list next to an open chat.
func (m Model) renderSidebar(width, height int) string {
	return m.renderList(width, height, m.focus == paneList)
}

func emptyListText(m Model) string {
	switch {
	case m.filter != "":
		return "  No chats match /" + m.filter
	case m.unreadOnly:
		return "  No unread chats. Press u to show all."
	case m.archive:
		return "  No archived chats. Press A to go back."
	case !m.status.Connected:
		return "  Connecting… chats will appear here."
	default:
		return "  No chats yet."
	}
}

// renderArchiveRow is the "Archived" entry at the top of the inbox.
func (m Model) renderArchiveRow(width int, sel bool) string {
	fill := paint(lipgloss.NewStyle(), sel)
	marker := fill.Render("  ")
	if sel {
		marker = paint(styleAccent, sel).Render("▌ ")
	}
	archivedUnread, mentioned := 0, false
	for _, c := range m.chats {
		if c.IsArchived && c.Unread > 0 {
			archivedUnread++
			mentioned = mentioned || c.Mentioned
		}
	}
	_, archived, _ := m.chatCounts()
	icon := lipgloss.NewStyle().Width(avatarBigCols).Align(lipgloss.Center)
	left1 := marker + paint(icon, sel).Render("📦") + fill.Render(" ") + paint(styleNameBold, sel).Render("Archived")
	right1 := paint(styleDim, sel).Render(fmt.Sprint(archived)) + fill.Render(" ")
	hint := "enter to open · A from anywhere"
	if archivedUnread > 0 {
		hint = fmt.Sprintf("%d with unread messages", archivedUnread)
	}
	left2 := marker + paint(icon, sel).Render("") + fill.Render(" ") + paint(styleDim, sel).Render(hint)
	right2 := fill.Render(" ")
	if mentioned {
		right2 = styleMentionBadge.Render("@") + fill.Render(" ")
	}
	return fitRow(left1, right1, width, fill) + "\n" + fitRow(left2, right2, width, fill)
}
