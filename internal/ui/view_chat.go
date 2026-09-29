package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// msgSpan records which content lines a message occupies in the viewport.
type msgSpan struct {
	idx        int // index into Model.msgs
	start, end int // first and last line (inclusive)
}

// Size limits for inline media, in cells.
const (
	imageMaxCols, imageMaxRows     = 40, 14
	stickerMaxCols, stickerMaxRows = 16, 7
)

// tagLabels turns the backend's "[IMAGE]"-style markers into friendlier text.
var tagLabels = []struct{ tag, label string }{
	{"[IMAGE]", "📷 Photo"},
	{"[STICKER]", "✨ Sticker"},
	{"[GIF]", "🎞 GIF"},
	{"[VIDEO]", "🎬 Video"},
	{"[VOICE NOTE]", "🎤 Voice note"},
	{"[AUDIO]", "🎵 Audio"},
	{"[DOCUMENT]", "📄"},
	{"[CONTACT]", "👤"},
	{"[LOCATION]", "📍"},
	{"[REACTION]", "Reacted"},
}

// splitTag returns the leading media tag of text (if any) and the rest.
func splitTag(text string) (label, rest string) {
	for _, t := range tagLabels {
		if strings.HasPrefix(text, t.tag) {
			return t.label, strings.TrimSpace(strings.TrimPrefix(text, t.tag))
		}
	}
	return "", text
}

// prettyTags replaces a leading media tag with its label.
func prettyTags(text string) string {
	label, rest := splitTag(text)
	switch {
	case label == "":
		return text
	case rest == "":
		return label
	}
	return label + " " + rest
}

func (m Model) bubbleMaxInner(width int) int {
	w := width*3/4 - 4 // border + padding
	if w < 16 {
		w = 16
	}
	return w
}

// mediaCells is the cell size used to show a message's media.
func (m Model) mediaCells(meta messages.MediaMeta, maxInner int) (cols, rows int) {
	maxC, maxR := imageMaxCols, imageMaxRows
	if meta.Type == messages.MediaSticker {
		maxC, maxR = stickerMaxCols, stickerMaxRows
	}
	if maxC > maxInner {
		maxC = maxInner
	}
	cw, ch := 8, 16
	if m.img != nil {
		cw, ch = m.img.cellW, m.img.cellH
	}
	return termimg.FitCells(meta.Width, meta.Height, maxC, maxR, cw, ch)
}

// mediaBlock renders a message's image area, or "" when it has none to show.
func (m Model) mediaBlock(msg messages.Message, maxInner int) (block string, meta messages.MediaMeta, ok bool) {
	if !m.img.enabled() {
		return "", meta, false
	}
	meta, ok = msg.MediaMeta()
	if !ok {
		return "", meta, false
	}
	cols, rows := m.mediaCells(meta, maxInner)
	e := m.img.get(imgKey{imgMessage, msg.Id, cols, rows})
	switch {
	case e != nil && e.state == imgReady:
		return e.text, meta, true
	case e != nil && e.state == imgFailed:
		return "", meta, false
	}
	// Reserve the space while loading so the layout doesn't jump.
	lines := make([]string, rows)
	for i := range lines {
		lines[i] = strings.Repeat(" ", cols)
	}
	lines[(rows-1)/2] = lipgloss.PlaceHorizontal(cols, lipgloss.Center, styleMuted.Render("loading…"))
	return strings.Join(lines, "\n"), meta, true
}

func (m Model) renderChatPane(width, height int) string {
	title := ""
	if m.current != nil {
		kind := "contact"
		if isGroup(m.current.JID) {
			kind = "group"
		}
		title = " " + m.avatarCells(m.current, avatarSmallCols, avatarSmallRows, false)[0] + " " +
			styleTitle.Render(chatName(m.current)) + styleDim.Render("  "+kind)
		if m.readReceiptsOff && kind == "contact" && !m.privacy.IsSelfChat(m.current.JID) {
			// explains why messages here stop at delivered
			title += styleMuted.Render("  ·  your read receipts are off: ") +
				statusMark(messages.StatusDelivered) + styleMuted.Render(" is as far as it goes")
		}
	}
	parts := []string{m.renderHeader(title, width, m.focus == paneMessages)}
	parts = append(parts, lipgloss.NewStyle().Width(width).Height(m.vp.Height).Render(m.vp.View()))
	if m.replyTo != nil {
		parts = append(parts, m.renderReplyBar(width))
	}
	if len(m.attachments) > 0 {
		parts = append(parts, m.renderAttachment(width))
	}
	if m.mention != nil {
		parts = append(parts, m.renderMentionPicker(width))
	}
	parts = append(parts, m.renderCompose(width))
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(strings.Join(parts, "\n"))
}

// renderCompose draws the input as a rounded box with the text centred on
// its middle line.
func (m Model) renderCompose(width int) string {
	border := pal.Muted
	if m.mode == modeInsert {
		border = colorWarm
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Width(width - 2).
		Render(m.compose.View())
}

func (m Model) renderMessages(width int) (string, []msgSpan) {
	if m.current == nil {
		return "", nil
	}
	if len(m.msgs) == 0 {
		return "\n" + styleDim.PaddingLeft(2).Width(width).Render(
			"No messages yet. Fetching recent history from your phone… (press i to write one)"), nil
	}
	group := isGroup(m.current.JID)
	now := time.Now()
	maxInner := m.bubbleMaxInner(width)

	var out []string
	lines := 0
	add := func(s string) {
		out = append(out, s)
		lines += strings.Count(s, "\n") + 1
	}
	spans := make([]msgSpan, 0, len(m.msgs))
	var lastDay time.Time
	lastSender := ""
	for i, msg := range m.msgs {
		t := toTime(int64(msg.Timestamp))
		if lastDay.IsZero() || !sameDay(t, lastDay) {
			add("")
			add(lipgloss.PlaceHorizontal(width, lipgloss.Center, styleDate.Render(" "+dayLabel(t, now)+" ")))
			lastDay = t
			lastSender = ""
		}
		sender := msg.ContactId
		if msg.FromMe {
			sender = "me"
		}
		// A blank line between all messages; two when the sender changes.
		add("")
		if sender != lastSender && lastSender != "" {
			add("")
		}
		start := lines
		selected := m.mode == modeVisual && i == m.sel
		add(m.renderBubble(msg, group && sender != lastSender, selected, maxInner, width, t))
		spans = append(spans, msgSpan{idx: i, start: start, end: lines - 1})
		lastSender = sender
	}
	add("")
	return strings.Join(out, "\n"), spans
}

// quoteBlock renders the message a reply quotes, WhatsApp-style.
func (m Model) quoteBlock(msg messages.Message, maxInner int) []string {
	if msg.QuotedID == "" {
		return nil
	}
	who := m.quotedSenderName(msg)
	bar := lipgloss.NewStyle().Foreground(senderColor(msg.QuotedSender))
	text := strings.ReplaceAll(prettyTags(msg.QuotedText), "\n", " ")
	if text == "" {
		text = "message"
	}
	return []string{
		bar.Render("▎") + senderStyle(msg.QuotedSender).Render(ansi.Truncate(who, maxInner-1, "…")),
		bar.Render("▎") + styleDim.Render(ansi.Truncate(text, maxInner-1, "…")),
	}
}

// quotedSenderName names a quoted message's author using names already on
// screen.
func (m Model) quotedSenderName(msg messages.Message) string {
	q := msg.QuotedSender
	if q == "" {
		if msg.FromMe {
			return chatName(m.current)
		}
		return "You"
	}
	for _, x := range m.msgs {
		if x.Id == msg.QuotedID {
			return m.senderName(x)
		}
	}
	user := strings.Split(q, "@")[0]
	for _, x := range m.msgs {
		if x.FromMe && strings.Split(x.ContactId, "@")[0] == user {
			return "You"
		}
		if !x.FromMe && strings.Split(x.ContactId, "@")[0] == user && x.ContactShort != "" {
			return x.ContactShort
		}
	}
	if m.current != nil && !isGroup(m.current.JID) && strings.HasPrefix(m.current.JID, user+"@") {
		return chatName(m.current)
	}
	return "+" + user
}

// reactionLine summarises reactions like "👍 2  ❤️".
func reactionLine(rs []messages.Reaction) string {
	if len(rs) == 0 {
		return ""
	}
	counts := map[string]int{}
	var order []string
	mine := map[string]bool{}
	for _, r := range rs {
		if counts[r.Emoji] == 0 {
			order = append(order, r.Emoji)
		}
		counts[r.Emoji]++
		if r.Sender == "" {
			mine[r.Emoji] = true
		}
	}
	var parts []string
	for _, e := range order {
		p := e
		if counts[e] > 1 {
			p += " " + fmt.Sprint(counts[e])
		}
		if mine[e] {
			parts = append(parts, lipgloss.NewStyle().Foreground(colorWarm).Render(p))
		} else {
			parts = append(parts, styleDim.Render(p))
		}
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderBubble(msg messages.Message, showSender, selected bool, maxInner, width int, t time.Time) string {
	border := pal.Muted
	stampStyle := styleStampThem
	if msg.FromMe {
		border, stampStyle = pal.Iris, styleStampMe
	}
	failed := msg.FromMe && msg.Status == messages.StatusFailed
	if failed {
		border = pal.Love
	}
	if selected {
		border = pal.Rose
	}

	block, meta, hasMedia := m.mediaBlock(msg, maxInner)
	label, text := splitTag(strings.TrimRight(msg.Text, "\n"))
	if !hasMedia && label != "" {
		text = prettyTags(msg.Text)
	}

	var lines []string
	if showSender && !msg.FromMe {
		name := msg.ContactShort
		if name == "" {
			name = msg.ContactName
		}
		lines = append(lines, senderStyle(msg.ContactId).Render(ansi.Truncate(name, maxInner, "…")))
	}
	if msg.Forwarded {
		lines = append(lines, styleMuted.Italic(true).Render("↪ Forwarded"))
	}
	lines = append(lines, m.quoteBlock(msg, maxInner)...)
	if hasMedia {
		lines = append(lines, strings.Split(block, "\n")...)
		switch meta.Type {
		case messages.MediaGIF:
			text = strings.TrimSpace("GIF  " + text)
		case messages.MediaVideo:
			dur := ""
			if meta.Seconds > 0 {
				dur = fmt.Sprintf(" %d:%02d", meta.Seconds/60, meta.Seconds%60)
			}
			text = strings.TrimSpace("▶ Video" + dur + "  " + text)
		}
	}
	if text != "" {
		if m.search != nil && matchesQuery(text, m.search.query) {
			// plain, so search highlights line up with what was typed
			for _, l := range strings.Split(ansi.Wrap(text, maxInner, ""), "\n") {
				lines = append(lines, m.highlight(l, selected, styleBase))
			}
		} else {
			lines = append(lines, formatText(text, maxInner, styleBase, msg.Mentions)...)
		}
	}

	stamp := stampStyle.Render(t.Format("15:04"))
	if msg.FromMe {
		if mark := statusMark(msg.Status); mark != "" {
			stamp += " " + mark
		}
	}
	inner := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > inner {
			inner = w
		}
	}
	// Put the time on the last text line when it fits, like WhatsApp does;
	// otherwise on its own line.
	sw := lipgloss.Width(stamp)
	if n := len(lines); n > 0 && text != "" && lipgloss.Width(lines[n-1])+2+sw <= maxInner {
		last := lines[n-1]
		if w := lipgloss.Width(last) + 2 + sw; w > inner {
			inner = w
		}
		lines[n-1] = last + strings.Repeat(" ", inner-lipgloss.Width(last)-sw) + stamp
	} else {
		if sw > inner {
			inner = sw
		}
		lines = append(lines, lipgloss.PlaceHorizontal(inner, lipgloss.Right, stamp))
	}

	if failed {
		lines = append(lines, styleErr.Render("not sent · v then R to retry"))
		if w := lipgloss.Width(lines[len(lines)-1]); w > inner {
			inner = w
		}
	}
	body := strings.Join(lines, "\n")
	var bubble string
	if hasMedia && meta.Type == messages.MediaSticker && !selected {
		bubble = body // stickers float without a bubble, like on the phone
	} else {
		bubble = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(border).
			Padding(0, 1).Width(inner + 2).
			Render(body)
	}
	if r := reactionLine(msg.Reactions); r != "" {
		align := lipgloss.Left
		if msg.FromMe {
			align = lipgloss.Right
		}
		bubble = lipgloss.JoinVertical(align, bubble, " "+r+" ")
	}
	if selected {
		// a marker in the gutter makes the selection easy to spot
		h := lipgloss.Height(bubble)
		gutter := lipgloss.NewStyle().Foreground(pal.Rose).Render(strings.TrimSuffix(strings.Repeat("▌\n", h), "\n"))
		if msg.FromMe {
			return lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.PlaceHorizontal(width-2, lipgloss.Right, bubble), gutter)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, gutter, bubble)
	}
	if msg.FromMe {
		return lipgloss.PlaceHorizontal(width-1, lipgloss.Right, bubble)
	}
	return lipgloss.NewStyle().MarginLeft(1).Render(bubble)
}

// statusMark shows how far one of your messages got, as two small blocks
// that fill up in Rosé Pine colours instead of ticks:
//
//	□□ muted   sending
//	■□ subtle  sent (reached WhatsApp)
//	■■ gold    delivered
//	■■ rose    read (iris: voice note or video played)
//	✕  love    not sent
func statusMark(status int) string {
	fg := func(c lipgloss.Color, s string) string { return lipgloss.NewStyle().Foreground(c).Render(s) }
	switch status {
	case messages.StatusPending:
		return fg(pal.Muted, "□□")
	case messages.StatusSent:
		return fg(pal.Subtle, "■") + fg(pal.Muted, "□")
	case messages.StatusDelivered:
		return fg(colorWarm, "■■")
	case messages.StatusRead:
		return fg(pal.Rose, "■■")
	case messages.StatusPlayed:
		return fg(pal.Iris, "■■")
	case messages.StatusFailed:
		return fg(pal.Love, "✕")
	}
	return ""
}
