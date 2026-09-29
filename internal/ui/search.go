package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Searcher searches a chat's whole history; *messages.SessionManager
// implements it. Without one, search covers the loaded messages only.
type Searcher interface {
	SearchHistory(ctx context.Context, chat, query string) ([]messages.Message, int, error)
}

// chatSearch is an in-chat search, vim-style.
type chatSearch struct {
	query   string
	matches []int // indices into Model.msgs, oldest first
	pos     int   // current match in matches
	// where the view was when the search started, for esc
	prevOffset int
	prevMode   mode
	prevSel    int
}

type searchResultMsg struct {
	chat, query string
	msgs        []messages.Message
	total       int
	err         error
}

// smartCase: lowercase queries match any case; an uppercase letter makes
// the search exact.
func smartCase(query string) (needle string, fold bool) {
	for _, r := range query {
		if unicode.IsUpper(r) {
			return query, false
		}
	}
	return strings.ToLower(query), true
}

func matchesQuery(text, query string) bool {
	if query == "" {
		return false
	}
	needle, fold := smartCase(query)
	if fold {
		text = strings.ToLower(text)
	}
	return strings.Contains(text, needle)
}

// startSearch opens the "/" prompt in the chat.
func (m *Model) startSearch() tea.Cmd {
	m.search = &chatSearch{prevOffset: m.vp.YOffset, prevMode: m.mode, prevSel: m.sel}
	m.picker = false
	m.mode = modeChatSearch
	m.cmdline.Prompt = "/"
	m.cmdline.SetValue("")
	return m.cmdline.Focus()
}

// findMatches recomputes matches over the loaded messages.
func (m *Model) findMatches() {
	s := m.search
	if s == nil {
		return
	}
	s.matches = s.matches[:0]
	for i, msg := range m.msgs {
		if matchesQuery(msg.Text, s.query) {
			s.matches = append(s.matches, i)
		}
	}
}

// jumpToMatch moves to match i (clamped) and selects it.
func (m *Model) jumpToMatch(i int) {
	s := m.search
	if s == nil || len(s.matches) == 0 {
		return
	}
	s.pos = min(max(i, 0), len(s.matches)-1)
	m.sel = s.matches[s.pos]
	m.refreshMessages(false)
	m.scrollToSelection()
}

// nearestMatch is the newest match at or above the selection (search goes
// back in time, like reading up the chat).
func (m Model) nearestMatch(from int) int {
	s := m.search
	for i := len(s.matches) - 1; i >= 0; i-- {
		if s.matches[i] <= from {
			return i
		}
	}
	return len(s.matches) - 1
}

func (m Model) handleChatSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.search
	switch msg.String() {
	case "esc":
		// cancel: back to where we were
		m.mode, m.sel = s.prevMode, s.prevSel
		m.search = nil
		m.cmdline.Blur()
		m.cmdline.Prompt = ":"
		m.refreshMessages(false)
		m.vp.SetYOffset(s.prevOffset)
		return m, nil
	case "enter":
		m.cmdline.Blur()
		m.cmdline.Prompt = ":"
		if s.query == "" {
			m.search = nil
			m.mode = s.prevMode
			return m, nil
		}
		m.mode = modeVisual
		if len(s.matches) > 0 {
			m.jumpToMatch(s.pos)
		}
		return m, m.searchHistory()
	case "ctrl+n", "up", "tab":
		// older match without leaving the prompt (the chat reads upwards)
		if len(s.matches) > 0 {
			m.jumpToMatch((s.pos - 1 + len(s.matches)) % len(s.matches))
		}
		return m, nil
	case "ctrl+p", "down", "shift+tab":
		if len(s.matches) > 0 {
			m.jumpToMatch((s.pos + 1) % len(s.matches))
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.cmdline, cmd = m.cmdline.Update(msg)
	if q := m.cmdline.Value(); q != s.query {
		s.query = q
		m.findMatches()
		if len(s.matches) > 0 {
			m.jumpToMatch(m.nearestMatch(len(m.msgs) - 1))
		} else {
			m.refreshMessages(false)
		}
	}
	return m, cmd
}

// searchHistory asks the backend for matches older than what's loaded.
func (m Model) searchHistory() tea.Cmd {
	if m.searcher == nil || m.current == nil || m.search == nil {
		return nil
	}
	sr, chat, q := m.searcher, m.current.JID, m.search.query
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, total, err := sr.SearchHistory(ctx, chat, q)
		return searchResultMsg{chat: chat, query: q, msgs: msgs, total: total, err: err}
	}
}

// applySearchResult loads older messages when the history has matches
// beyond the loaded ones, keeping the current match selected.
func (m Model) applySearchResult(r searchResultMsg) (tea.Model, tea.Cmd) {
	s := m.search
	if s == nil || m.current == nil || r.chat != m.current.JID || r.query != s.query {
		return m, nil // stale
	}
	if r.err != nil {
		m.notice, m.noticeErr = r.err.Error(), true
		return m, nil
	}
	if len(r.msgs) > len(m.msgs) {
		var curID string
		if cur, ok := m.selected(); ok {
			curID = cur.Id
		}
		m.msgs = r.msgs
		m.findMatches()
		m.sel = len(m.msgs) - 1
		pos := m.nearestMatch(len(m.msgs) - 1)
		for i, idx := range s.matches {
			if m.msgs[idx].Id == curID {
				pos = i
			}
		}
		m.refreshMessages(false)
		m.jumpToMatch(pos)
	}
	if len(s.matches) == 0 {
		m.notice, m.noticeErr = "no messages match /"+s.query, true
		m.mode = modeNormal
		m.search = nil
		m.refreshMessages(false)
	}
	return m, nil
}

// nextMatch moves to an older (dir -1) or newer (dir +1) match, wrapping
// around like vim.
func (m *Model) nextMatch(dir int) {
	s := m.search
	if s == nil || len(s.matches) == 0 {
		m.notice, m.noticeErr = "no search: press / to search", true
		return
	}
	i := (s.pos + dir + len(s.matches)) % len(s.matches)
	if m.mode != modeVisual {
		m.mode = modeVisual
	}
	m.jumpToMatch(i)
}

// searchStatus is shown on the command line while a search is active.
func (m Model) searchStatus() string {
	s := m.search
	if s == nil || s.query == "" {
		return ""
	}
	if len(s.matches) == 0 {
		return styleErr.Render("/" + s.query + "  no matches")
	}
	return styleFilter.Render("/"+s.query) +
		styleDim.Render(fmt.Sprintf("  match %d/%d · n older · N newer · esc clear", s.pos+1, len(s.matches)))
}

// highlight marks occurrences of the search query in one line of text;
// the current match is rose, others gold.
func (m Model) highlight(line string, current bool, base lipgloss.Style) string {
	if m.search == nil {
		return base.Render(line)
	}
	mark := lipgloss.NewStyle().Foreground(pal.Base).Background(colorWarm)
	if current {
		mark = lipgloss.NewStyle().Foreground(pal.Base).Background(pal.Rose).Bold(true)
	}
	return highlightWith(line, m.search.query, base, mark)
}
