package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/skratchdot/open-golang/open"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// PictureSource fetches full-size profile pictures;
// *messages.SessionManager implements it.
type PictureSource interface {
	FullPicture(ctx context.Context, jid string) (string, error)
}

// pictureView is the full-screen profile picture (V in the chat list).
type pictureView struct {
	conv    *messages.Conversation
	loading bool
	path    string // "" when the chat has no picture
	image   string // rendered picture
	w, h    int    // picture size in pixels
	err     error
}

type pictureMsg struct {
	jid        string
	path, text string
	w, h       int
	err        error
}

func (m *Model) openPicture(c *messages.Conversation) tea.Cmd {
	if c == nil {
		return nil
	}
	if m.pictures == nil {
		m.notice, m.noticeErr = "profile pictures aren't available", true
		return nil
	}
	m.pic = &pictureView{conv: c, loading: true}
	ps, im, jid := m.pictures, m.img, c.JID
	// leave room for the name, caption, footer and the blank lines between
	cols, rows := m.width-4, m.mainHeight()-6
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		path, err := ps.FullPicture(ctx, jid)
		if err != nil || path == "" {
			return pictureMsg{jid: jid, err: err}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return pictureMsg{jid: jid, err: err}
		}
		img, err := termimg.Decode(data)
		if err != nil {
			return pictureMsg{jid: jid, err: err}
		}
		msg := pictureMsg{jid: jid, path: path, w: img.Bounds().Dx(), h: img.Bounds().Dy()}
		if im.enabled() {
			c, r := termimg.FitCells(msg.w, msg.h, cols, rows, im.cellW, im.cellH)
			msg.text, msg.err = im.render(img, c, r)
		}
		return msg
	}
}

func (m Model) applyPicture(r pictureMsg) (tea.Model, tea.Cmd) {
	if m.pic == nil || m.pic.conv.JID != r.jid {
		return m, nil
	}
	m.pic.loading, m.pic.err = false, r.err
	m.pic.path, m.pic.image, m.pic.w, m.pic.h = r.path, r.text, r.w, r.h
	return m, nil
}

func (m Model) handlePicture(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "V", "backspace":
		m.pic = nil
	case "o":
		if m.pic.path != "" {
			path := m.pic.path
			return m, func() tea.Msg {
				return actionDoneMsg{ok: "Opened the picture", err: open.Start(path)}
			}
		}
	}
	return m, nil
}

func (m Model) renderPicture(width, height int) string {
	p := m.pic
	name := styleTitle.Foreground(pal.Rose).Render(chatName(p.conv))
	var body, caption string
	switch {
	case p.loading:
		body = styleDim.Render("Loading picture…")
	case p.err != nil:
		body = styleErr.Render(p.err.Error())
	case p.path == "":
		// no picture: the big letter avatar
		body = strings.Join(m.avatarCells(p.conv, avatarBigCols, avatarBigRows, false), "\n")
		caption = styleDim.Render("No profile picture")
	case p.image == "":
		body = styleDim.Render("Images are off; press o to open it.")
	default:
		body = p.image
	}
	if caption == "" && p.w > 0 {
		caption = styleDim.Render(fmt.Sprintf("%d×%d", p.w, p.h))
	}
	footer := styleMuted.Render("esc close")
	if p.path != "" {
		footer = styleMuted.Render("o open in viewer · esc close")
	}
	block := lipgloss.JoinVertical(lipgloss.Center, name, "", body, "", caption, footer)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}
