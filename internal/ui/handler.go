package ui

import (
	"regexp"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/skratchdot/open-golang/open"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// Messages delivered from the backend to the Bubble Tea program.
type (
	newMessageMsg messages.Message
	screenMsg     []messages.Message
	chatListMsg   []*messages.Conversation
	statusMsg     messages.SessionStatus
	noticeMsg     struct {
		text  string
		isErr bool
	}
	qrMsg struct {
		qr      string
		attempt int
		timeout int
	}
	clearMsg struct{}
	helpMsg  struct{}
	quitMsg  struct{}
)

// tviewTag matches the tview color tags the backend embeds in status text,
// e.g. "[red]", "[-]", "[::b]", "[#ff0000:-:-]". Only lowercase names are
// matched so message placeholders like "[IMAGE]" survive.
var tviewTag = regexp.MustCompile(`\[(-|[a-z]+|#[0-9a-fA-F]{6})?(:(-|[a-z]+|#[0-9a-fA-F]{6})?)?(:[-a-z]*)?\]`)

func stripTags(s string) string { return tviewTag.ReplaceAllString(s, "") }

// Handler implements messages.UiMessageHandler by forwarding every backend
// callback into the Bubble Tea event loop, preserving their order.
type Handler struct {
	send func(tea.Msg)
}

// NewHandler returns a handler that delivers messages with send (usually
// (*tea.Program).Send). send may be set later with SetSend.
func NewHandler(send func(tea.Msg)) *Handler { return &Handler{send: send} }

// SetSend sets the function used to deliver messages to the UI.
func (h *Handler) SetSend(send func(tea.Msg)) { h.send = send }

func (h *Handler) NewMessage(m messages.Message)   { h.send(newMessageMsg(m)) }
func (h *Handler) NewScreen(ms []messages.Message) { h.send(screenMsg(ms)) }
func (h *Handler) SetChats([]messages.Chat)        {}
func (h *Handler) UpdateChatList(cs []*messages.Conversation) {
	h.send(chatListMsg(cs))
}
func (h *Handler) PrintError(err error) {
	if err != nil {
		h.send(noticeMsg{text: stripTags(err.Error()), isErr: true})
	}
}
func (h *Handler) PrintText(s string)                 { h.send(noticeMsg{text: stripTags(s)}) }
func (h *Handler) PrintFile(path string)              { h.send(noticeMsg{text: "file: " + path}) }
func (h *Handler) PrintQR(qr string)                  { h.send(qrMsg{qr: qr}) }
func (h *Handler) SetStatus(s messages.SessionStatus) { h.send(statusMsg(s)) }
func (h *Handler) OpenFile(path string)               { _ = open.Run(path) }
func (h *Handler) ShowColorList()                     {}
func (h *Handler) Clear()                             { h.send(clearMsg{}) }
func (h *Handler) PrintCommands()                     { h.send(helpMsg{}) }
func (h *Handler) PrintHelp()                         { h.send(helpMsg{}) }
func (h *Handler) Quit()                              { h.send(quitMsg{}) }
func (h *Handler) UpdateQR(qr string, attempt, timeout int) {
	h.send(qrMsg{qr: qr, attempt: attempt, timeout: timeout})
}
