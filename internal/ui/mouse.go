package ui

import tea "github.com/charmbracelet/bubbletea"

// Mouse support is deliberately small: click a chat to open it, and the
// wheel scrolls what's under the pointer. Everything else is keyboard.

// wheelLines is how far one wheel notch scrolls the messages.
const wheelLines = 3

// overlayOpen reports screens that cover the chat list and messages.
func (m Model) overlayOpen() bool {
	return m.showHelp || m.info != nil || m.global != nil || m.fwd != nil || m.stk != nil || m.pic != nil || m.qr != ""
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.overlayOpen() {
		return m, nil
	}
	overList := m.screen == screenList || msg.X < m.sidebarW
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		dir := 1
		if msg.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		if overList {
			m.scrollList(dir)
		} else if m.current != nil {
			if dir < 0 {
				m.vp.LineUp(wheelLines)
			} else {
				m.vp.LineDown(wheelLines)
			}
		}
		return m, nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress || !overList {
			return m, nil
		}
		// entries start below the list header, fullItemHeight rows each
		row := msg.Y - headerRows
		if row < 0 {
			return m, nil
		}
		i := m.listOffset + row/fullItemHeight
		chats := m.visibleChats()
		if i >= len(chats) {
			return m, nil
		}
		if m.mode == modeVisual || m.mode == modeInsert {
			m.mode = modeNormal
			m.compose.Blur()
		}
		m.cursor = i
		return m, m.openChat(chats[i])
	}
	return m, nil
}

// scrollList moves the chat list one entry, keeping the cursor on screen.
func (m *Model) scrollList(dir int) {
	n := len(m.visibleChats())
	rows := m.listRows()
	m.listOffset = min(max(m.listOffset+dir, 0), max(n-rows, 0))
	if m.cursor < m.listOffset {
		m.cursor = m.listOffset
	}
	if m.cursor >= m.listOffset+rows {
		m.cursor = m.listOffset + rows - 1
	}
}
