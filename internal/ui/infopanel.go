package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/skratchdot/open-golang/open"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// Size of the picture in the info panel, in cells.
const infoPicCols, infoPicRows = 24, 12

// infoState is the open info panel.
type infoState struct {
	conv    *messages.Conversation
	loading bool
	err     error
	data    messages.ChatInfo
	picture string // rendered picture
	scroll  int    // first shown participant
}

type infoMsg struct {
	jid     string
	data    messages.ChatInfo
	picture string
	err     error
}

// openInfo shows the info panel for c and starts fetching its details.
func (m *Model) openInfo(c *messages.Conversation) tea.Cmd {
	if c == nil {
		return nil
	}
	m.info = &infoState{conv: c, loading: true}
	if m.actions == nil {
		m.info.loading = false
		m.info.err = fmt.Errorf("chat info isn't available")
		return nil
	}
	a, im, jid := m.actions, m.img, c.JID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, err := a.ChatInfo(ctx, jid)
		msg := infoMsg{jid: jid, data: data, err: err}
		if err == nil && data.Picture != "" && im.enabled() {
			if raw, rerr := os.ReadFile(data.Picture); rerr == nil {
				if img, derr := termimg.Decode(raw); derr == nil {
					cols, rows := termimg.FitCells(img.Bounds().Dx(), img.Bounds().Dy(), infoPicCols, infoPicRows, im.cellW, im.cellH)
					msg.picture, _ = im.render(img, cols, rows)
				}
			}
		}
		return msg
	}
}

func (m Model) handleInfo(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	in := m.info
	switch msg.String() {
	case "esc", "q", "backspace", "K":
		m.info = nil
	case "j", "down":
		if in.scroll < len(in.data.Participants)-1 {
			in.scroll++
		}
	case "k", "up":
		if in.scroll > 0 {
			in.scroll--
		}
	case "o":
		if in.data.Picture != "" {
			path := in.data.Picture
			return m, func() tea.Msg {
				return actionDoneMsg{ok: "Opened the profile picture", err: open.Start(path)}
			}
		}
		m.notice, m.noticeErr = "no profile picture", true
	}
	return m, nil
}

func (m Model) renderInfo(width, height int) string {
	in := m.info
	name := chatName(in.conv)
	if in.data.Name != "" {
		name = in.data.Name
	}
	kind := "Contact"
	if isGroup(in.conv.JID) {
		kind = "Group"
	}
	label := lipgloss.NewStyle().Foreground(pal.Iris).Bold(true)
	textW := min(width-4, 72)

	var right []string
	right = append(right, styleTitle.Foreground(pal.Rose).Render(name), styleMuted.Render(kind), "")
	switch {
	case in.loading:
		right = append(right, styleDim.Render("Loading…"))
	case in.err != nil:
		right = append(right, styleErr.Render(in.err.Error()))
	default:
		d := in.data
		if d.Phone != "" {
			right = append(right, label.Render("Phone"), styleBase.Render(d.Phone), "")
		}
		about := "About"
		if d.IsGroup {
			about = "Description"
		}
		right = append(right, label.Render(about))
		if strings.TrimSpace(d.About) == "" {
			right = append(right, styleMuted.Render("—"))
		} else {
			right = append(right, styleBase.Width(textW).Render(strings.TrimSpace(d.About)))
		}
		if !d.Created.IsZero() {
			right = append(right, "", label.Render("Created"), styleBase.Render(d.Created.Format("2 Jan 2006")))
		}
	}

	pic := in.picture
	if pic == "" {
		pic = strings.Join(m.avatarCells(in.conv, avatarBigCols, avatarBigRows, false), "\n")
	}
	top := lipgloss.JoinHorizontal(lipgloss.Top, pic, "   ", strings.Join(right, "\n"))

	var members []string
	if d := in.data; d.IsGroup && len(d.Participants) > 0 {
		members = append(members, "", label.Render(fmt.Sprintf("%d members · %d admins", len(d.Participants), d.Admins)))
		room := height - lipgloss.Height(top) - 5
		for i := in.scroll; i < len(d.Participants) && len(members)-2 < room; i++ {
			members = append(members, styleBase.Render("  "+ansi.Truncate(d.Participants[i], textW, "…")))
		}
	}
	footer := styleMuted.Render("esc close · o open picture")
	if len(in.data.Participants) > 0 {
		footer = styleMuted.Render("esc close · j/k scroll members · o open picture")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, top, strings.Join(members, "\n"), "", footer)
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Padding(1, 2).Render(body)
}
