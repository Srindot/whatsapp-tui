package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// GlobalSearcher searches messages across all chats and loads a hit's
// chat; *messages.SessionManager implements it.
type GlobalSearcher interface {
	SearchAll(ctx context.Context, query string) ([]messages.SearchHit, error)
	LoadAround(ctx context.Context, chat, msgID string) ([]messages.Message, error)
}

// globalSearch is the "search all messages" screen.
type globalSearch struct {
	query   string
	typing  bool // the prompt has focus; otherwise j/k move through hits
	hits    []messages.SearchHit
	cursor  int
	offset  int
	loading bool
	err     error
	seq     int // drops results of queries typed over
}

type globalHitsMsg struct {
	seq  int
	hits []messages.SearchHit
	err  error
}

type openHitMsg struct {
	hit  messages.SearchHit
	msgs []messages.Message
	err  error
}

// globalRowHeight is the lines per result: chat + time, sender: text, divider.
const globalRowHeight = 3

func (m *Model) openGlobalSearch() tea.Cmd {
	if m.globalSearcher == nil {
		m.notice, m.noticeErr = "searching all chats isn't available", true
		return nil
	}
	if m.global == nil {
		m.global = &globalSearch{}
	}
	m.global.typing = true
	m.mode = modeGlobalSearch
	m.cmdline.Prompt = "search all: "
	m.cmdline.SetValue(m.global.query)
	m.cmdline.CursorEnd()
	return m.cmdline.Focus()
}

func (m *Model) closeGlobalSearch() {
	m.global = nil
	m.mode = modeNormal
	m.cmdline.Blur()
	m.cmdline.Prompt = ":"
}

// runGlobalSearch queries the database for the current text.
func (m *Model) runGlobalSearch() tea.Cmd {
	g := m.global
	g.seq++
	g.cursor, g.offset = 0, 0
	if strings.TrimSpace(g.query) == "" {
		g.hits, g.loading, g.err = nil, false, nil
		return nil
	}
	g.loading = true
	seq, q, gs := g.seq, g.query, m.globalSearcher
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hits, err := gs.SearchAll(ctx, q)
		return globalHitsMsg{seq: seq, hits: hits, err: err}
	}
}

func (m Model) applyGlobalHits(r globalHitsMsg) (tea.Model, tea.Cmd) {
	g := m.global
	if g == nil || r.seq != g.seq {
		return m, nil // closed, or typed over
	}
	g.loading, g.err = false, r.err
	// Narrow the database's case-insensitive LIKE with smartcase.
	g.hits = g.hits[:0]
	for _, h := range r.hits {
		if matchesQuery(h.Text, g.query) {
			g.hits = append(g.hits, h)
		}
	}
	return m, nil
}

func (m Model) globalRows() int {
	return max((m.mainHeight()-headerRows-2)/globalRowHeight, 1)
}

func (m Model) handleGlobalSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := m.global
	key := msg.String()
	if g.typing {
		switch key {
		case "esc":
			m.closeGlobalSearch()
			return m, nil
		case "enter", "down", "ctrl+n", "ctrl+j":
			// to the results (enter on a single hit opens it)
			g.typing = false
			m.cmdline.Blur()
			if key == "enter" && len(g.hits) == 1 {
				return m, m.openHit(g.hits[0])
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.cmdline, cmd = m.cmdline.Update(msg)
		if q := m.cmdline.Value(); q != g.query {
			g.query = q
			return m, tea.Batch(cmd, m.runGlobalSearch())
		}
		return m, cmd
	}

	move := func(d int) {
		g.cursor = min(max(g.cursor+d, 0), max(len(g.hits)-1, 0))
		rows := m.globalRows()
		if g.cursor < g.offset {
			g.offset = g.cursor
		}
		if g.cursor >= g.offset+rows {
			g.offset = g.cursor - rows + 1
		}
	}
	switch key {
	case "esc", "q", "backspace":
		m.closeGlobalSearch()
	case "/", "i", "S":
		g.typing = true
		return m, m.cmdline.Focus()
	case "j", "down", "ctrl+n":
		move(1)
	case "k", "up", "ctrl+p":
		move(-1)
	case "ctrl+d":
		move(m.globalRows() / 2)
	case "ctrl+u":
		move(-m.globalRows() / 2)
	case "G":
		move(len(g.hits))
	case "g":
		move(-len(g.hits))
	case "enter", "l":
		if g.cursor < len(g.hits) {
			return m, m.openHit(g.hits[g.cursor])
		}
	}
	return m, nil
}

// openHit loads the hit's chat around the message.
func (m Model) openHit(h messages.SearchHit) tea.Cmd {
	gs := m.globalSearcher
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := gs.LoadAround(ctx, h.ChatId, h.Id)
		return openHitMsg{hit: h, msgs: msgs, err: err}
	}
}

// showHit opens the chat with the hit selected in visual mode and the
// query highlighted, so n/N continue within the chat.
func (m Model) showHit(r openHitMsg) (tea.Model, tea.Cmd) {
	if r.err != nil {
		m.notice, m.noticeErr = r.err.Error(), true
		return m, nil
	}
	query := ""
	if m.global != nil {
		query = m.global.query
	}
	var conv *messages.Conversation
	for _, c := range m.chats {
		if c.JID == r.hit.ChatId {
			conv = c
			break
		}
	}
	if conv == nil {
		conv = &messages.Conversation{JID: r.hit.ChatId, Name: r.hit.ChatName}
	}
	m.closeGlobalSearch()
	cmd := m.openChat(conv)
	m.msgs = r.msgs
	m.sel = len(m.msgs) - 1
	for i, x := range m.msgs {
		if x.Id == r.hit.Id {
			m.sel = i
		}
	}
	m.mode = modeVisual
	m.search = &chatSearch{query: query, prevMode: modeNormal}
	m.findMatches()
	for i, idx := range m.search.matches {
		if idx == m.sel {
			m.search.pos = i
		}
	}
	m.refreshMessages(false)
	m.scrollToSelection()
	return m, cmd
}

// highlightQuery marks occurrences of query in line in gold.
func highlightQuery(line, query string, base lipgloss.Style) string {
	return highlightWith(line, query, base, lipgloss.NewStyle().Foreground(pal.Base).Background(colorWarm))
}

// highlightWith renders line in base with each occurrence of query (smartcase)
// in mark.
func highlightWith(line, query string, base, mark lipgloss.Style) string {
	if query == "" {
		return base.Render(line)
	}
	needle, fold := smartCase(query)
	hay := line
	if fold {
		hay = strings.ToLower(line)
	}
	var b strings.Builder
	for {
		i := strings.Index(hay, needle)
		// ToLower can change byte lengths for some scripts; only
		// highlight when offsets still line up.
		if i < 0 || len(hay) != len(line) {
			b.WriteString(base.Render(line))
			return b.String()
		}
		b.WriteString(base.Render(line[:i]))
		b.WriteString(mark.Render(line[i : i+len(needle)]))
		line, hay = line[i+len(needle):], hay[i+len(needle):]
		if line == "" {
			return b.String()
		}
	}
}

// snippet centres the text around the first match so it stays visible.
func snippet(text, query string, width int) string {
	text = strings.Join(strings.Fields(prettyTags(text)), " ")
	if lipgloss.Width(text) <= width || query == "" {
		return ansi.Truncate(text, width, "…")
	}
	needle, fold := smartCase(query)
	hay := text
	if fold {
		hay = strings.ToLower(text)
	}
	i := strings.Index(hay, needle)
	if i < 0 || len(hay) != len(text) {
		return ansi.Truncate(text, width, "…")
	}
	start := max(0, i-width/3)
	for start > 0 && start < len(text) && (text[start]&0xC0) == 0x80 {
		start-- // don't cut a UTF-8 sequence
	}
	out := text[start:]
	if start > 0 {
		out = "…" + out
	}
	return ansi.Truncate(out, width, "…")
}

func (m Model) renderGlobalSearch(width, height int) string {
	g := m.global
	title := styleTitle.Foreground(pal.Rose).Render(" Search all messages")
	switch {
	case g.loading:
		title += styleDim.Render("  searching…")
	case g.query != "":
		n := fmt.Sprintf("  %d results", len(g.hits))
		if len(g.hits) == 1 {
			n = "  1 result"
		}
		title += styleDim.Render(n)
	}
	var b strings.Builder
	b.WriteString(m.renderHeader(title, width, true))
	switch {
	case g.err != nil:
		b.WriteString("\n\n  " + styleErr.Render(g.err.Error()))
	case strings.TrimSpace(g.query) == "":
		b.WriteString("\n\n" + styleDim.Render("  Type to search every chat. enter / ↓ to go to the results, esc to close."))
	case len(g.hits) == 0 && !g.loading:
		b.WriteString("\n\n" + styleDim.Render("  No messages match."))
	}
	now := time.Now()
	divider := "  " + lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", max(width-3, 0)))
	for i := g.offset; i < len(g.hits) && i < g.offset+m.globalRows(); i++ {
		h := g.hits[i]
		sel := i == g.cursor && !g.typing
		fill := paint(lipgloss.NewStyle(), sel)
		marker := fill.Render("  ")
		if sel {
			marker = paint(styleAccent, sel).Render("▌ ")
		}
		chat := paint(senderStyle(h.ChatId), sel).Render(h.ChatName)
		when := paint(styleDim, sel).Render(listTime(int64(h.Timestamp), now)+" "+toTime(int64(h.Timestamp)).Format("15:04")) + fill.Render(" ")
		who := m.senderName(h.Message)
		if isGroup(h.ChatId) || h.FromMe {
			who += ": "
		} else {
			who = ""
		}
		prefix := paint(styleMuted, sel).Render(who)
		text := highlightQuery(snippet(h.Text, g.query, width-lipgloss.Width(who)-4), g.query, paint(styleBase, sel))
		b.WriteString("\n" + fitRow(marker+chat, when, width, fill))
		b.WriteString("\n" + fitRow(marker+prefix+text, fill.Render(" "), width, fill))
		b.WriteString("\n" + divider)
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(b.String())
}
