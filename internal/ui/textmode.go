package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// textHint lists the keys of the input box's normal mode.
const textHint = "i insert · a append · I/A line start/end · o new line · h l w b 0 $ move · x D dd delete · enter send · ctrl+a attach · : command · esc leave"

// boxKey sends a key the textarea understands (arrows, alt+f, ctrl+k, …).
func (m *Model) boxKey(k tea.KeyMsg) {
	m.compose, _ = m.compose.Update(k)
}

func runeKey(r rune, alt bool) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: alt}
}

// handleText handles keys in the input box's normal mode (R): vim-style
// moves and edits, and i / a / I / A / o to start typing.
func (m Model) handleText(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.pendingD {
		m.pendingD = false
		if key == "d" { // dd: clear the line
			m.compose.CursorStart()
			m.boxKey(tea.KeyMsg{Type: tea.KeyCtrlK})
			m.fitCompose()
		}
		return m, nil
	}
	insert := func(m Model) (tea.Model, tea.Cmd) {
		m.mode = modeInsert
		return m, m.compose.Focus()
	}
	switch key {
	case "esc":
		// leave the box; a half-done edit isn't kept for enter to save later
		if m.editing != nil {
			m.cancelEdit()
		}
		m.mode = modeNormal
		m.compose.Blur()
	case "i":
		return insert(m)
	case "a":
		m.boxKey(tea.KeyMsg{Type: tea.KeyRight})
		return insert(m)
	case "I":
		m.compose.CursorStart()
		return insert(m)
	case "A":
		m.compose.CursorEnd()
		return insert(m)
	case "o":
		m.compose.CursorEnd()
		m.compose.InsertRune('\n')
		m.fitCompose()
		return insert(m)
	case "C":
		m.boxKey(tea.KeyMsg{Type: tea.KeyCtrlK})
		return insert(m)
	case "h", "left":
		m.boxKey(tea.KeyMsg{Type: tea.KeyLeft})
	case "l", "right":
		m.boxKey(tea.KeyMsg{Type: tea.KeyRight})
	case "w":
		m.boxKey(runeKey('f', true))
	case "b":
		m.boxKey(runeKey('b', true))
	case "0", "^", "home":
		m.compose.CursorStart()
	case "$", "end":
		m.compose.CursorEnd()
	case "j", "down":
		m.compose.CursorDown()
	case "k", "up":
		m.compose.CursorUp()
	case "x", "delete":
		m.boxKey(tea.KeyMsg{Type: tea.KeyDelete})
		m.fitCompose()
	case "X":
		m.boxKey(tea.KeyMsg{Type: tea.KeyBackspace})
		m.fitCompose()
	case "D":
		m.boxKey(tea.KeyMsg{Type: tea.KeyCtrlK})
		m.fitCompose()
	case "d":
		m.pendingD = true
	case "enter":
		// send, and stay in the box
		next, cmd := m.handleInsert(msg)
		nm := next.(Model)
		nm.mode = modeText
		return nm, cmd
	case ":":
		m.compose.Blur()
		m.mode = modeCommand
		m.cmdline.SetValue("")
		return m, m.cmdline.Focus()
	case "?":
		m.showHelp = true
	case "ctrl+a":
		return m, m.pickFiles()
	case "ctrl+v":
		m.mode = modeInsert
		return m, m.paste()
	}
	return m, nil
}
