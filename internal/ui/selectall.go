package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handleSelectAll handles a key while the whole input is selected (ctrl+a),
// like a normal text box: typing or pasting replaces the text, backspace and
// delete clear it, ctrl+x cuts it, esc deselects. done is false for keys that
// just deselect and then act as usual (arrows, enter, …).
func (m Model) handleSelectAll(msg tea.KeyMsg) (_ tea.Model, _ tea.Cmd, done bool) {
	switch msg.String() {
	case "ctrl+a":
		return m, nil, true
	case "esc":
		m.selectAll = false
		return m, nil, true
	case "backspace", "delete", "ctrl+h", "ctrl+backspace", "ctrl+w":
		m.clearCompose()
		return m, m.updateMentions(), true
	case "ctrl+x":
		text, clip := m.compose.Value(), m.clip
		m.clearCompose()
		return m, func() tea.Msg { return actionDoneMsg{ok: "Cut", err: clip.WriteText(text)} }, true
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace || msg.Paste {
		m.clearCompose()
		next, cmd := m.handleInsert(msg) // then type it
		return next, cmd, true
	}
	return m, nil, false
}

func (m *Model) clearCompose() {
	m.selectAll = false
	m.compose.SetValue("")
	m.chosen, m.mention = nil, nil
	m.fitCompose()
}

// renderSelectedCompose draws the input with all its text highlighted (the
// textarea can't show a selection itself).
func (m Model) renderSelectedCompose() string {
	sel := lipgloss.NewStyle().Background(colorSelBg).Foreground(pal.Text)
	lines := strings.Split(m.compose.Value(), "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		prompt := "  "
		if i == 0 {
			prompt = "❯ "
		}
		out[i] = styleAccent.Render(prompt) + sel.Render(l)
	}
	return strings.Join(out, "\n")
}
