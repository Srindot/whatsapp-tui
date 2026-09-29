package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// StickerSender sends stickers and GIFs; *messages.SessionManager
// implements it.
type StickerSender interface {
	RecentMedia(ctx context.Context, mediaType string, limit int) ([]messages.Message, error)
	SendExisting(ctx context.Context, chat, msgID string) error
	SendNewSticker(ctx context.Context, chat, path string) error
	SendNewGIF(ctx context.Context, chat, path, caption string) error
}

const recentLimit = 60

// Grid cell sizes (cells) for the two tabs, plus a gap.
const (
	stickerCellCols, stickerCellRows = 12, 6
	gifCellCols, gifCellRows         = 18, 8
	gridGap                          = 2
)

type stickerTab int

const (
	tabStickers stickerTab = iota
	tabGIFs
)

// stickerPicker is the sticker/GIF tray.
type stickerPicker struct {
	tab     stickerTab
	items   [2][]messages.Message
	cursor  [2]int
	offset  [2]int // first visible row
	loading bool
	err     error
	// creating a new one from a file (yazi) goes to this tab
	newFor stickerTab
}

type recentMediaMsg struct {
	stickers, gifs []messages.Message
	err            error
}

func (m *Model) openStickers() tea.Cmd {
	if m.stickers == nil || m.current == nil {
		m.notice, m.noticeErr = "stickers aren't available", true
		return nil
	}
	m.stk = &stickerPicker{loading: true}
	ss := m.stickers
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		st, err := ss.RecentMedia(ctx, messages.MediaSticker, recentLimit)
		if err != nil {
			return recentMediaMsg{err: err}
		}
		gifs, err := ss.RecentMedia(ctx, messages.MediaGIF, recentLimit)
		return recentMediaMsg{stickers: st, gifs: gifs, err: err}
	}
}

func (p *stickerPicker) cell() (cols, rows int) {
	if p.tab == tabGIFs {
		return gifCellCols, gifCellRows
	}
	return stickerCellCols, stickerCellRows
}

// grid returns how many items fit per row and how many rows are visible.
func (m Model) stickerGrid() (perRow, rows int) {
	cols, cellRows := m.stk.cell()
	perRow = max((m.width-2)/(cols+gridGap), 1)
	rows = max((m.mainHeight()-headerRows-3)/(cellRows+2), 1) // +caption +gap
	return perRow, rows
}

func (m Model) handleStickers(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.stk
	items := p.items[p.tab]
	perRow, rows := m.stickerGrid()
	move := func(d int) {
		if len(items) == 0 {
			return
		}
		c := min(max(p.cursor[p.tab]+d, 0), len(items)-1)
		p.cursor[p.tab] = c
		r := c / perRow
		if r < p.offset[p.tab] {
			p.offset[p.tab] = r
		}
		if r >= p.offset[p.tab]+rows {
			p.offset[p.tab] = r - rows + 1
		}
	}
	switch msg.String() {
	case "esc", "q", "s":
		m.stk = nil
	case "tab", "shift+tab":
		p.tab = 1 - p.tab
	case "l", "right":
		move(1)
	case "h", "left":
		move(-1)
	case "j", "down":
		move(perRow)
	case "k", "up":
		move(-perRow)
	case "g":
		move(-len(items))
	case "G":
		move(len(items))
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		sel, ss, chat := items[p.cursor[p.tab]], m.stickers, m.current.JID
		m.stk = nil
		m.vp.GotoBottom()
		return m, m.action("", func(ctx context.Context) (string, error) {
			return "", ss.SendExisting(ctx, chat, sel.Id)
		})
	case "n":
		// make a new one from a file
		p.newFor = p.tab
		return m, m.pickFiles()
	case "p":
		if p.tab == tabStickers {
			cmd := m.pasteSticker()
			m.stk = nil
			return m, cmd
		}
	}
	return m, nil
}

// sendNewFromFiles turns files picked in yazi into stickers or GIFs.
func (m Model) sendNewFromFiles(paths []string, tab stickerTab) tea.Cmd {
	ss, chat := m.stickers, m.current.JID
	label := "Sticker sent"
	if tab == tabGIFs {
		label = "GIF sent"
	}
	if len(paths) > 1 {
		label = fmt.Sprintf("%d sent", len(paths))
	}
	return m.action(label, func(ctx context.Context) (string, error) {
		var errs []error
		for _, p := range paths {
			var err error
			if tab == tabGIFs {
				err = ss.SendNewGIF(ctx, chat, p, "")
			} else {
				err = ss.SendNewSticker(ctx, chat, p)
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
		return "", errors.Join(errs...)
	})
}

// pasteSticker turns the clipboard image into a sticker.
func (m Model) pasteSticker() tea.Cmd {
	clip, ss, chat := m.clip, m.stickers, m.current.JID
	return m.action("Sticker sent", func(ctx context.Context) (string, error) {
		data, _, err := clip.Image()
		if err != nil {
			return "", fmt.Errorf("paste: %w", err)
		}
		f, err := os.CreateTemp("", "whatsapp-tui-paste-*")
		if err != nil {
			return "", err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(data); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
		return "", ss.SendNewSticker(ctx, chat, f.Name())
	})
}

func (m Model) renderStickers(width, height int) string {
	p := m.stk
	tab := func(label string, n int, active bool) string {
		if active {
			return styleTitle.Foreground(pal.Rose).Render(label) + styleDim.Render(fmt.Sprintf(" %d", n))
		}
		return styleMuted.Render(fmt.Sprintf("%s %d", label, n))
	}
	title := " " + tab("Stickers", len(p.items[tabStickers]), p.tab == tabStickers) +
		styleMuted.Render("  ·  ") + tab("GIFs", len(p.items[tabGIFs]), p.tab == tabGIFs)
	if p.loading {
		title += styleDim.Render("  loading…")
	}
	var b strings.Builder
	b.WriteString(m.renderHeader(title, width, true))

	items := p.items[p.tab]
	switch {
	case p.err != nil:
		b.WriteString("\n\n  " + styleErr.Render(p.err.Error()))
	case !p.loading && len(items) == 0:
		what := "stickers"
		if p.tab == tabGIFs {
			what = "GIFs"
		}
		b.WriteString("\n\n" + styleDim.Render("  No "+what+" yet: ones you receive or send show up here.") +
			"\n" + styleDim.Render("  Press n to make one from a file."))
	}
	cols, cellRows := p.cell()
	perRow, rows := m.stickerGrid()
	start := p.offset[p.tab] * perRow
	for r := 0; r < rows && start+r*perRow < len(items); r++ {
		var cells []string
		for c := 0; c < perRow; c++ {
			i := start + r*perRow + c
			if i >= len(items) {
				break
			}
			cells = append(cells, m.stickerCell(items[i], cols, cellRows, i == p.cursor[p.tab]), strings.Repeat(" ", gridGap))
		}
		b.WriteString("\n\n " + lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(b.String())
}

// stickerCell draws one item with a frame when selected.
func (m Model) stickerCell(msg messages.Message, cols, rows int, sel bool) string {
	var body string
	if meta, ok := msg.MediaMeta(); ok && m.img.enabled() {
		c, r := m.mediaCells(meta, cols)
		c, r = min(c, cols), min(r, rows)
		if e := m.img.get(imgKey{imgMessage, msg.Id, c, r}); e != nil && e.state == imgReady {
			body = e.text
		}
	}
	if body == "" {
		label := "✨"
		if msg.MediaType == messages.MediaGIF {
			label = "GIF"
		}
		body = lipgloss.Place(cols, rows, lipgloss.Center, lipgloss.Center, styleMuted.Render(label))
	}
	body = lipgloss.Place(cols, rows, lipgloss.Center, lipgloss.Center, body)
	border := lipgloss.HiddenBorder()
	style := lipgloss.NewStyle().Border(border)
	if sel {
		style = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(pal.Rose)
	}
	return style.Render(body)
}

// stickerLoads lists the visible items and their image size, for loading.
func (m Model) stickerLoads() []struct {
	msg        messages.Message
	cols, rows int
} {
	p := m.stk
	cols, cellRows := p.cell()
	perRow, rows := m.stickerGrid()
	items := p.items[p.tab]
	var out []struct {
		msg        messages.Message
		cols, rows int
	}
	start := p.offset[p.tab] * perRow
	for i := start; i < len(items) && i < start+perRow*rows; i++ {
		meta, ok := items[i].MediaMeta()
		if !ok {
			continue
		}
		c, r := m.mediaCells(meta, cols)
		out = append(out, struct {
			msg        messages.Message
			cols, rows int
		}{items[i], min(c, cols), min(r, cellRows)})
	}
	return out
}
