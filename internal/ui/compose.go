package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

// composeMaxLines is how tall the input box may grow before it scrolls.
const composeMaxLines = 6

func newCompose() textarea.Model {
	ta := textarea.New()
	ta.Prompt = "❯ "
	ta.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "❯ "
		}
		return "  "
	})
	ta.Placeholder = composePlaceholder
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.SetHeight(1)
	// Enter sends (handled by the model). A new line is alt+enter or ctrl+j;
	// kitty can map shift+enter to alt+enter (see the README).
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ta.KeyMap.Paste.SetEnabled(false) // ctrl+v is handled by the model (images too)

	styles := func(s *textarea.Style) {
		s.Base = lipgloss.NewStyle()
		s.CursorLine = lipgloss.NewStyle()
		s.Text = styleBase
		s.Placeholder = styleMuted
		s.Prompt = styleAccent
		s.EndOfBuffer = lipgloss.NewStyle()
	}
	styles(&ta.FocusedStyle)
	styles(&ta.BlurredStyle)
	ta.Cursor.Style = lipgloss.NewStyle().Foreground(pal.Rose)
	ta.Blur()
	return ta
}

// composeLines is how many screen lines the current text needs.
func (m Model) composeLines() int {
	w := m.compose.Width()
	if w <= 0 {
		return 1
	}
	n := 0
	for _, line := range strings.Split(m.compose.Value(), "\n") {
		n += max(1, (lipgloss.Width(line)+w)/w) // +cursor room at the end
	}
	return min(max(n, 1), composeMaxLines)
}

// growCompose makes the input box full height before an edit, so the
// textarea never scrolls its first lines out of view while the box is still
// growing; fitCompose shrinks it back afterwards.
func (m *Model) growCompose() {
	m.compose.SetHeight(composeMaxLines)
}

// fitCompose resizes the input box to its content and the layout with it.
func (m *Model) fitCompose() {
	if h := m.composeLines(); h != m.compose.Height() {
		m.compose.SetHeight(h)
		m.resize()
	}
}
