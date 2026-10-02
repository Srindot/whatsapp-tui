package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// Layout constants (rows).
const (
	bottomRows     = 2 // status line + command line
	headerRows     = 2 // pane title + separator
	fullItemHeight = 3 // name/time line + preview line + divider
)

func (m Model) mainHeight() int {
	h := m.height - bottomRows
	if h < 0 {
		return 0
	}
	return h
}

// listRows is how many chats fit in the list for the current screen.
func (m Model) listRows() int {
	return (m.mainHeight() - headerRows) / fullItemHeight
}

func (m Model) rightWidth() int {
	w := m.width - m.sidebarW - 1 // 1 column for the divider
	if w < 10 {
		w = 10
	}
	return w
}

func (m *Model) resize() {
	m.vp.Width = m.rightWidth()
	m.vp.Height = m.mainHeight() - headerRows - (m.compose.Height() + 2) - m.attachRows() - m.replyRows() - m.mentionPickerRows()
	if m.vp.Height < 1 {
		m.vp.Height = 1
	}
	// box border (2) + padding (2) + prompt + room for the cursor
	m.compose.SetWidth(m.rightWidth() - 4)
	m.cmdline.Width = m.width - 2
	m.img.resize()
	m.clampCursor()
}

// refreshMessages re-renders the message list into the viewport.
func (m *Model) refreshMessages(gotoBottom bool) {
	content, spans := m.renderMessages(m.rightWidth())
	m.vp.SetContent(content)
	m.msgSpans = spans
	m.msgLines = strings.Split(content, "\n")
	if gotoBottom {
		m.vp.GotoBottom()
	}
}

// ---------- helpers ----------

func chatName(c *messages.Conversation) string {
	if c == nil {
		return ""
	}
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	if i := strings.IndexByte(c.JID, '@'); i > 0 {
		return "+" + c.JID[:i]
	}
	return c.JID
}

func isGroup(jid string) bool { return strings.HasSuffix(jid, messages.GROUPSUFFIX) }

func toTime(ts int64) time.Time {
	if ts > 1e12 { // milliseconds
		ts /= 1000
	}
	return time.Unix(ts, 0)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// listTime formats a chat's last activity like WhatsApp does.
func listTime(ts int64, now time.Time) string {
	if ts <= 0 {
		return ""
	}
	t := toTime(ts)
	switch {
	case sameDay(t, now):
		return t.Format("15:04")
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case now.Sub(t) < 7*24*time.Hour:
		return t.Format("Monday")
	default:
		return t.Format("02/01/06")
	}
}

func dayLabel(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return "Today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case t.Year() == now.Year():
		return t.Format("Mon, 2 Jan")
	default:
		return t.Format("Mon, 2 Jan 2006")
	}
}

// initial is the letter on a chat's text avatar; "#" for names without a
// letter (phone numbers, emoji).
func initial(name string) string {
	if r, ok := termimg.AvatarInitial(name); ok {
		return string(r)
	}
	return "#"
}

// paint adds the selection background to a style when sel is true.
func paint(s lipgloss.Style, sel bool) lipgloss.Style {
	if sel {
		return s.Background(colorSelBg)
	}
	return s
}

// fitRow lays out left and right text in exactly width cells.
func fitRow(left, right string, width int, fill lipgloss.Style) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 1 {
		left = ansi.Truncate(left, width-rw-2, "…")
		gap = width - lipgloss.Width(left) - rw
		if gap < 0 {
			gap = 0
		}
	}
	return left + fill.Render(strings.Repeat(" ", gap)) + right
}

// ---------- login / help ----------

func (m Model) renderQR(width, height int) string {
	head := styleTitle.Render("Link this terminal to WhatsApp")
	steps := styleDim.Render("On your phone: Settings → Linked devices → Link a device")
	count := ""
	if m.qrTimeout > 0 {
		count = styleDim.Render(fmt.Sprintf("code %d · refreshes in %ds", m.qrAttempt, m.qrTimeout))
	} else if m.qrAttempt > 0 {
		count = styleDim.Render(fmt.Sprintf("code %d · waiting for a new code…", m.qrAttempt))
	}
	block := lipgloss.JoinVertical(lipgloss.Center, head, steps, "", strings.TrimRight(m.qr, "\n"), "", count)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// ---------- status line ----------

func (m Model) renderStatusLine() string {
	var badge string
	switch {
	case m.view != nil:
		badge = styleModeVisual.Render("VIEW")
	case m.stk != nil:
		badge = styleModeVisual.Render("STICKERS")
	case m.fwd != nil:
		badge = styleModeVisual.Render("FORWARD")
	default:
		badge = m.modeBadge()
	}
	return m.statusLineWith(badge)
}

func (m Model) modeBadge() string {
	var badge string
	switch m.mode {
	case modeInsert:
		badge = styleModeInsert.Render("INSERT")
	case modeText:
		badge = styleModeNormal.Render("TEXT")
	case modeCommand:
		badge = styleModeCmd.Render("COMMAND")
	case modeFilter:
		badge = styleModeSearch.Render("SEARCH")
	case modeVisual:
		badge = styleModeVisual.Render("VISUAL")
	case modeChatSearch, modeGlobalSearch:
		badge = styleModeSearch.Render("SEARCH")
	default:
		badge = styleModeNormal.Render("NORMAL")
	}
	return badge
}

// statusLineWith draws the status line with the given mode badge.
func (m Model) statusLineWith(badge string) string {
	where := " chats"
	if m.screen == screenChat && m.current != nil {
		where = " " + chatName(m.current)
	}
	conn := styleErr.Background(colorBarBg).Render("○ offline")
	if m.status.Connected {
		conn = styleOnline.Background(colorBarBg).Render("● online")
	}
	left := badge + styleStatusBar.Render(where)
	right := conn + styleStatusBar.Render("  ? help ")
	return fitRow(left, right, m.width, styleStatusBar)
}

// visualHint lists the visual-mode actions.
const visualHint = "j/k gg/G move · enter reply · p private · r react · e edit · f forward · space view · y copy · s save · d delete · o open · esc"

func (m Model) renderCommandLine() string {
	switch {
	case m.confirm != nil:
		return m.renderConfirm()
	case m.stk != nil && m.notice == "":
		hint := "hjkl move · tab stickers/GIFs · enter send · n new from a file · esc close"
		if m.stk.tab == tabStickers {
			hint = "hjkl move · tab stickers/GIFs · enter send · n new from a file · p paste as sticker · esc close"
		}
		return styleDim.Render(ansi.Truncate(hint, m.width, "…"))
	case m.fwd != nil:
		return m.cmdlineCompact() + styleDim.Render("  ctrl+n/p move · space pick several · enter send · esc cancel")
	case m.global != nil && m.global.typing:
		return m.cmdline.View()
	case m.global != nil && m.notice == "":
		return styleDim.Render(ansi.Truncate("j/k move · enter open in chat · / edit search · esc close", m.width, "…"))
	case m.mode == modeCommand || m.mode == modeFilter:
		return m.cmdline.View()
	case m.mode == modeChatSearch:
		s := m.search
		switch {
		case s == nil || s.query == "":
			return m.cmdlineCompact() + styleDim.Render("  type to search this chat")
		case len(s.matches) == 0:
			return m.cmdlineCompact() + styleErr.Render("  no matches loaded — enter searches the whole chat")
		}
		return m.cmdlineCompact() + styleDim.Render(fmt.Sprintf(
			"  %d/%d · ctrl+n older · ctrl+p newer · enter select (then n/N)", s.pos+1, len(s.matches)))
	case m.search != nil && m.notice == "" && !m.picker:
		return m.searchStatus()
	case m.mode == modeVisual && m.picker:
		return m.renderPicker()
	case m.mode == modeVisual && m.notice == "":
		return styleDim.Render(ansi.Truncate(visualHint, m.width, "…"))
	case m.mode == modeText && m.notice == "":
		return styleDim.Render(ansi.Truncate(textHint, m.width, "…"))
	case m.notice != "" && m.noticeErr:
		return styleErr.Render(ansi.Truncate(m.notice, m.width, "…"))
	case m.notice != "":
		return styleDim.Render(ansi.Truncate(m.notice, m.width, "…"))
	}
	return ""
}

// cmdlineCompact renders the command line only as wide as its text, so a
// hint can follow it on the same line.
func (m Model) cmdlineCompact() string {
	c := m.cmdline
	c.Width = lipgloss.Width(c.Value()) + 1
	return c.View()
}

// View renders the whole screen.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	h := m.mainHeight()
	var main string
	switch {
	case m.showHelp:
		main = m.renderHelp(m.width, h)
	case m.view != nil:
		main = m.renderMediaView(m.width, h)
	case m.pic != nil:
		main = m.renderPicture(m.width, h)
	case m.stk != nil:
		main = m.renderStickers(m.width, h)
	case m.fwd != nil:
		main = m.renderForward(m.width, h)
	case m.info != nil:
		main = m.renderInfo(m.width, h)
	case m.global != nil:
		main = m.renderGlobalSearch(m.width, h)
	case m.qr != "":
		main = m.renderQR(m.width, h)
	case m.screen == screenList:
		main = m.renderFullList(m.width, h)
	default:
		divider := lipgloss.NewStyle().Foreground(colorBorder).
			Render(strings.TrimSuffix(strings.Repeat("│\n", h), "\n"))
		main = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderSidebar(m.sidebarW, h), divider, m.renderChatPane(m.rightWidth(), h))
	}
	main = lipgloss.NewStyle().Width(m.width).Height(h).MaxHeight(h).Render(main)
	cmdline := lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(m.renderCommandLine())
	return paintBackground(main+"\n"+m.renderStatusLine()+"\n"+cmdline, m.bgSeq)
}

// backgroundSeq returns the SGR sequence for the theme's base background in
// the terminal's colour profile, or "" when disabled or unsupported.
func backgroundSeq(enabled bool) string {
	if !enabled {
		return ""
	}
	seq := lipgloss.ColorProfile().Color(string(pal.Base)).Sequence(true)
	if seq == "" {
		return ""
	}
	return "\x1b[" + seq + "m"
}

// sgrReset matches SGR sequences that reset the background: full resets
// ("\x1b[m", "\x1b[0m") and default-background ("\x1b[49m").
var sgrReset = regexp.MustCompile(`\x1b\[(0?|49)m`)

// paintBackground fills the screen with bg: it starts every line with bg and
// re-applies it after each reset, so unstyled cells and padding get the theme
// background instead of the terminal's.
func paintBackground(s, bg string) string {
	if bg == "" {
		return s
	}
	s = sgrReset.ReplaceAllStringFunc(s, func(r string) string { return r + bg })
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = bg + l + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}
