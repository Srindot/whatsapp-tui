package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// helpSections is every key, grouped by where it works. Keep it in sync
// with the handlers (and the README).
var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Chat list", [][2]string{
		{"j k · gg G", "move · top / bottom"},
		{"ctrl+d ctrl+u", "half page down / up"},
		{"enter / l", "open chat"},
		{"backspace / q", "back · quit from the list"},
		{"/", "find chats & contacts · enter browses them · esc clears"},
		{"S", "search messages in ALL chats"},
		{"u", "only unread chats"},
		{"A", "archived chats (or the Archived row)"},
		{"K", "chat info: picture, description"},
		{"V", "profile picture, full screen"},
		{"d then enter", "delete the chat"},
	}},
	{"In a chat", [][2]string{
		{"j k · gg G", "scroll · top / bottom"},
		{"ctrl+d ctrl+u", "half page"},
		{"ctrl+f ctrl+b", "full page"},
		{"h / l / tab", "chat list / messages"},
		{"i / enter", "write a message"},
		{"v", "select messages (visual)"},
		{"/ · n N", "search this chat · older / newer"},
		{"S", "search all chats"},
		{"@", "jump to messages that mention you"},
		{"A", "archived chats in the sidebar"},
		{"a", "attach files (yazi)"},
		{"drag & drop", "drop files on the window to attach them"},
		{"s", "stickers & GIFs"},
		{"p / ctrl+v", "paste screenshot or text"},
		{"K", "chat info"},
		{"ctrl+x", "drop attachment / cancel reply"},
		{"backspace / q", "back to the list"},
	}},
	{"Insert (typing)", [][2]string{
		{"enter", "send"},
		{"shift+enter", "new line (alt+enter, ctrl+j)"},
		{"ctrl+v", "paste image or text"},
		{"ctrl+a", "attach files"},
		{"@ (groups)", "mention: tab pick, ctrl+n/p move"},
		{"ctrl+x", "drop attachment / cancel reply"},
		{"esc", "back to normal mode"},
	}},
	{"Visual (v)", [][2]string{
		{"j k · gg G", "select another message"},
		{"r / enter", "reply"},
		{"p", "reply privately (groups)"},
		{"e", "react: 1–6, x remove, or type"},
		{"f", "forward (space picks several)"},
		{"y", "copy text / image"},
		{"s", "save (download) media"},
		{"d", "delete: enter for me, e for everyone"},
		{"space", "view photo/sticker/GIF full screen, play video"},
		{"o", "open media, or the message's link"},
		{"R", "retry a message that failed"},
		{"/ · n N", "search · older / newer"},
		{"esc", "done"},
	}},
	{"Search", [][2]string{
		{"/ (list)", "chat names"},
		{"/ (chat)", "this chat, whole history"},
		{"S · :search <text>", "every chat"},
		{"ctrl+n ctrl+p", "older / newer while typing"},
		{"enter", "select the match (visual)"},
		{"n N", "older / newer match"},
		{"esc esc", "cancel · clear highlights"},
	}},
	{"Trays & panels", [][2]string{
		{"sticker tray (s)", "hjkl move · tab stickers/GIFs"},
		{"", "enter send · n new from file"},
		{"", "p paste as sticker · esc close"},
		{"forward (f)", "type to filter · space pick"},
		{"", "ctrl+n/p move · enter send"},
		{"all-chat search (S)", "j/k move · enter open · / edit"},
		{"info (K)", "j/k members · o open picture"},
		{"picture (V)", "o open in viewer · esc close"},
		{"viewer (space)", "o open in its app · s save · esc"},
		{"help (?)", "j/k scroll · esc close"},
		{"mouse", "click a chat · wheel scrolls"},
		{"", "click a link to open it"},
	}},
	{"Commands", [][2]string{
		{":q", "quit"},
		{":search <text>", "search all chats"},
		{":attach [path…]", "attach (no path: yazi)"},
		{":sticker [path]", "sticker tray / make one"},
		{":gif [path]", "GIF tray / make one"},
		{":download-dir [dir]", "show / set download folder"},
		{":info", "chat info"},
		{":unread", "only unread chats"},
		{":archive :inbox", "switch chat list"},
		{":backlog", "fetch older messages"},
		{":read", "mark chat as read"},
		{":logout", "unlink this device"},
		{":help / ?", "this screen"},
	}},
}

// helpLines lays the sections out in as many columns as fit, and returns
// the lines (the screen scrolls them when they don't fit).
func (m Model) helpLines(width int) []string {
	keyW, descW := 20, 32
	colW := keyW + descW + 3
	keyStyle := lipgloss.NewStyle().Foreground(colorWarm).Width(keyW)
	title := lipgloss.NewStyle().Foreground(pal.Iris).Bold(true)

	var blocks [][]string
	for _, s := range helpSections {
		lines := []string{title.Render(s.title)}
		for _, k := range s.keys {
			lines = append(lines, keyStyle.Render(ansi.Truncate(k[0], keyW-1, "…"))+
				styleDim.Render(ansi.Truncate(k[1], descW, "…")))
		}
		blocks = append(blocks, append(lines, ""))
	}

	// fill columns top to bottom, balancing their heights
	ncols := max(1, min(len(blocks), (width-4)/colW))
	total := 0
	for _, b := range blocks {
		total += len(b)
	}
	target := (total + ncols - 1) / ncols
	cols := make([][]string, ncols)
	c := 0
	for _, b := range blocks {
		if c < ncols-1 && len(cols[c]) > 0 && len(cols[c])+len(b) > target {
			c++
		}
		cols[c] = append(cols[c], b...)
	}
	var rendered []string
	for _, col := range cols {
		rendered = append(rendered, lipgloss.NewStyle().Width(colW).Render(strings.Join(col, "\n")))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	var legend []string
	for _, st := range []struct {
		status int
		label  string
	}{
		{messages.StatusPending, "sending"}, {messages.StatusSent, "sent"},
		{messages.StatusDelivered, "delivered"}, {messages.StatusRead, "read"},
		{messages.StatusPlayed, "played"}, {messages.StatusFailed, "not sent"},
	} {
		legend = append(legend, statusMark(st.status)+" "+styleDim.Render(st.label))
	}
	status := title.Render("Your messages") + "   " + strings.Join(legend, styleMuted.Render("  ·  "))
	if m.readReceiptsOff {
		status += "\n" + styleMuted.Render("Your read receipts are off in WhatsApp, so it doesn't send you others' either:\n"+
			"one-to-one chats stop at delivered. Groups and played voice notes still show.")
	}
	return strings.Split(lipgloss.JoinVertical(lipgloss.Left, body, status), "\n")
}

func (m Model) renderHelp(width, height int) string {
	lines := m.helpLines(width)
	room := helpRoom(height)
	footer := "press ? or esc to close"
	if len(lines) > room {
		maxScroll := len(lines) - room
		scroll := min(m.helpScroll, maxScroll)
		lines = lines[scroll : scroll+room]
		footer = "j/k scroll · " + footer
		if scroll < maxScroll {
			footer = "↓ more · " + footer
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"), "", styleDim.Render(footer))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top,
		lipgloss.NewStyle().PaddingTop(1).Render(body))
}

// helpRoom is how many help lines fit: minus the top padding, the blank
// line and the footer.
func helpRoom(height int) int { return max(height-3, 1) }

// helpMaxScroll is how far the help screen can scroll.
func (m Model) helpMaxScroll() int {
	return max(len(m.helpLines(m.width))-helpRoom(m.mainHeight()), 0)
}
