package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Srindot/whatsapp-tui/internal/clipboard"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// Sender sends media; *messages.SessionManager implements it.
type Sender interface {
	SendImage(ctx context.Context, chat string, data []byte, caption string) error
	SendDocument(ctx context.Context, chat, path, caption string) error
}

// Clipboard reads the system clipboard; swappable for tests.
type Clipboard interface {
	Image() ([]byte, string, error)
	Text() (string, error)
	WriteText(text string) error
	WriteImage(data []byte, mime, text string) error
}

type systemClipboard struct{}

func (systemClipboard) Image() ([]byte, string, error) { return clipboard.Image() }
func (systemClipboard) Text() (string, error)          { return clipboard.Text() }
func (systemClipboard) WriteText(text string) error    { return clipboard.WriteText(text) }
func (systemClipboard) WriteImage(d []byte, mime, t string) error {
	return clipboard.WriteImage(d, mime, t)
}

// Attachment preview size, in cells.
const attachPreviewCols, attachPreviewRows = 12, 4

// attachment is a file or pasted image waiting to be sent.
type attachment struct {
	data          []byte // pasted images: the image; files: nil (read when sent)
	mime          string
	path          string // files: where it is; "" for pasted images
	name          string
	size          int64
	image         bool // sent as a photo (otherwise as a document)
	width, height int
	preview       string // rendered image, at most attachPreviewRows lines
}

type (
	// pasteImageMsg carries a pasted image; preview is rendered already.
	pasteImageMsg struct{ att *attachment }
	pasteTextMsg  string
	pasteFailMsg  struct{ err error }
	sentMsg       struct {
		n   int
		err error
	}
)

// paste reads the clipboard: an image becomes an attachment, text is typed.
func (m Model) paste() tea.Cmd {
	clip, im := m.clip, m.img
	return func() tea.Msg {
		data, mime, err := clip.Image()
		if err == nil {
			img, derr := termimg.Decode(data)
			if derr != nil {
				return pasteFailMsg{fmt.Errorf("pasted image: %w", derr)}
			}
			att := &attachment{data: data, mime: mime, image: true, name: "Pasted image", size: int64(len(data)),
				width: img.Bounds().Dx(), height: img.Bounds().Dy()}
			if im.enabled() {
				cols, rows := termimg.FitCells(att.width, att.height, attachPreviewCols, attachPreviewRows, im.cellW, im.cellH)
				if text, rerr := im.render(img, cols, rows); rerr == nil {
					att.preview = text
				}
			}
			return pasteImageMsg{att}
		}
		if !errors.Is(err, clipboard.ErrEmpty) {
			return pasteFailMsg{err}
		}
		text, err := clip.Text()
		if err != nil {
			if errors.Is(err, clipboard.ErrEmpty) {
				return pasteFailMsg{errors.New("clipboard is empty")}
			}
			return pasteFailMsg{err}
		}
		return pasteTextMsg(text)
	}
}

// insertText types s at the cursor.
func (m *Model) insertText(s string) {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", "    ").Replace(s)
	m.growCompose()
	m.compose.InsertString(s)
	m.fitCompose()
}

// addAttachments queues attachments above the input box.
func (m *Model) addAttachments(as ...*attachment) {
	m.attachments = append(m.attachments, as...)
	m.attachmentsChanged()
}

// dropAttachment removes the most recently added attachment.
func (m *Model) dropAttachment() {
	if n := len(m.attachments); n > 0 {
		m.attachments = m.attachments[:n-1]
		m.attachmentsChanged()
	}
}

func (m *Model) clearAttachments() {
	m.attachments = nil
	m.attachmentsChanged()
}

func (m *Model) attachmentsChanged() {
	switch len(m.attachments) {
	case 0:
		m.compose.Placeholder = composePlaceholder
	case 1:
		m.compose.Placeholder = "add a caption… (enter to send, ctrl+x to remove)"
	default:
		m.compose.Placeholder = "add a caption for the first one… (enter sends all)"
	}
	m.resize()
	m.refreshMessages(true)
}

// sendAttachments sends every queued attachment in order; the caption goes
// with the first one (the rest are sent without, like on the phone).
func (m Model) sendAttachments(caption string) tea.Cmd {
	if m.sender == nil || m.current == nil || len(m.attachments) == 0 {
		return nil
	}
	s, chat, atts := m.sender, m.current.JID, append([]*attachment(nil), m.attachments...)
	return func() tea.Msg {
		var errs []error
		for i, a := range atts {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			c := ""
			if i == 0 {
				c = caption
			}
			var err error
			switch {
			case a.image && a.data != nil:
				err = s.SendImage(ctx, chat, a.data, c)
			case a.image:
				var data []byte
				if data, err = os.ReadFile(a.path); err == nil {
					err = s.SendImage(ctx, chat, data, c)
				}
			default:
				err = s.SendDocument(ctx, chat, a.path, c)
			}
			cancel()
			if err != nil {
				errs = append(errs, err)
			}
		}
		return sentMsg{n: len(atts), err: errors.Join(errs...)}
	}
}

func (m Model) attachRows() int {
	if len(m.attachments) == 0 {
		return 0
	}
	return attachPreviewRows
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// fileIcon picks an icon for a document by its extension.
func fileIcon(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return "📕"
	case ".doc", ".docx", ".odt", ".txt", ".md", ".rtf":
		return "📝"
	case ".xls", ".xlsx", ".ods", ".csv":
		return "📊"
	case ".ppt", ".pptx", ".odp":
		return "📽"
	case ".zip", ".tar", ".gz", ".7z", ".rar", ".xz":
		return "🗜"
	case ".mp4", ".mkv", ".mov", ".webm", ".avi":
		return "🎬"
	case ".mp3", ".ogg", ".opus", ".m4a", ".wav", ".flac":
		return "🎵"
	}
	return "📄"
}

// renderAttachment shows the queued attachments above the input box as a
// row of cards: a preview for images, an icon for documents.
func (m Model) renderAttachment(width int) string {
	cardW := 26
	var cards []string
	used := 1
	for i, a := range m.attachments {
		kind := "document"
		if a.image {
			kind = "photo"
			if a.width > 0 {
				kind = fmt.Sprintf("%d×%d", a.width, a.height)
			}
		}
		info := []string{
			styleTitle.Render(ansi.Truncate(a.name, cardW-2, "…")),
			styleDim.Render(kind + " · " + humanSize(a.size)),
		}
		if i == len(m.attachments)-1 {
			info = append(info, styleMuted.Render("ctrl+x remove"))
		}
		var card string
		if a.preview != "" {
			card = lipgloss.JoinHorizontal(lipgloss.Top, a.preview, " ", strings.Join(info, "\n"))
		} else {
			icon := lipgloss.NewStyle().Width(3).Render(fileIcon(a.name))
			if a.image {
				icon = lipgloss.NewStyle().Width(3).Render("🖼")
			}
			card = lipgloss.JoinHorizontal(lipgloss.Top, icon, strings.Join(info, "\n"))
		}
		w := lipgloss.Width(card) + 3
		if used+w > width && len(cards) > 0 {
			cards = append(cards, styleDim.Render(fmt.Sprintf("+%d more", len(m.attachments)-i)))
			break
		}
		used += w
		cards = append(cards, card, "   ")
	}
	header := styleUnread.Render(fmt.Sprintf("📎 %d to send", len(m.attachments)))
	if len(m.attachments) == 1 {
		header = ""
	}
	block := lipgloss.JoinHorizontal(lipgloss.Top, append([]string{" "}, cards...)...)
	if header != "" {
		block = lipgloss.JoinVertical(lipgloss.Left, " "+header, block)
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Height(attachPreviewRows).MaxHeight(attachPreviewRows).
		Render(block)
}
