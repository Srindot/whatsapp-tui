package ui

import (
	"regexp"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Mouse support is deliberately small: click a chat to open it, and the
// wheel scrolls what's under the pointer. Everything else is keyboard.

// wheelLines is how far one wheel notch scrolls the messages.
const wheelLines = 3

// overlayOpen reports screens that cover the chat list and messages.
func (m Model) overlayOpen() bool {
	return m.showHelp || m.info != nil || m.global != nil || m.fwd != nil || m.stk != nil || m.pic != nil || m.view != nil || m.qr != ""
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
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if !overList {
			// a click on a link in the messages opens it
			if url := m.linkAt(msg.X, msg.Y); url != "" {
				return m, func() tea.Msg { return actionDoneMsg{ok: "Opened " + url, err: openURL(url)} }
			}
			return m, nil
		}
		// entries start below the list header, fullItemHeight rows each
		row := msg.Y - headerRows
		if row < 0 {
			return m, nil
		}
		i := m.listOffset + row/fullItemHeight
		if i >= m.listLen() {
			return m, nil
		}
		if m.mode == modeVisual || m.mode == modeInsert {
			m.mode = modeNormal
			m.compose.Blur()
		}
		m.cursor = i
		return m, m.openSelected()
	}
	return m, nil
}

// scrollList moves the chat list one entry, keeping the cursor on screen.
func (m *Model) scrollList(dir int) {
	n := m.listLen()
	rows := m.listRows()
	m.listOffset = min(max(m.listOffset+dir, 0), max(n-rows, 0))
	if m.cursor < m.listOffset {
		m.cursor = m.listOffset
	}
	if m.cursor >= m.listOffset+rows {
		m.cursor = m.listOffset + rows - 1
	}
}

// osc8 matches a hyperlink start ("\x1b]8;params;url" + ST) or end (empty url).
var osc8Seq = regexp.MustCompile("^\x1b\\]8;[^;]*;([^\x1b\x07]*)(?:\x1b\\\\|\x07)")

// linkAt returns the URL of the link drawn at screen cell (x, y) in the
// message pane, or "".
func (m Model) linkAt(x, y int) string {
	if m.screen != screenChat || m.current == nil {
		return ""
	}
	row := y - headerRows
	col := x - (m.sidebarW + 1) // sidebar + divider
	if row < 0 || row >= m.vp.Height || col < 0 {
		return ""
	}
	i := m.vp.YOffset + row
	if i >= len(m.msgLines) {
		return ""
	}
	return urlAtColumn(m.msgLines[i], col)
}

// urlAtColumn walks a rendered line and returns the hyperlink covering the
// given display column.
func urlAtColumn(line string, col int) string {
	url, pos := "", 0
	for len(line) > 0 {
		if loc := osc8Seq.FindStringSubmatchIndex(line); loc != nil {
			url = line[loc[2]:loc[3]]
			line = line[loc[1]:]
			continue
		}
		if line[0] == 0x1b { // other escape codes take no space
			n := ansiSeqLen(line)
			line = line[n:]
			continue
		}
		r, size := utf8.DecodeRuneInString(line)
		w := ansi.StringWidth(string(r))
		if col >= pos && col < pos+w {
			return url
		}
		pos += w
		line = line[size:]
	}
	return ""
}

// ansiSeqLen is the length of the escape sequence at the start of s.
func ansiSeqLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[': // CSI: ends with a byte in 0x40..0x7e
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
	case ']', '_', 'P': // OSC/APC/DCS: end with BEL or ESC \
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
	}
	return 2
}
