package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

type screen int

const (
	screenList screen = iota // full-screen chat list (start screen)
	screenChat               // sidebar + open chat
)

type pane int

const (
	paneList pane = iota
	paneMessages
)

type mode int

const (
	modeNormal mode = iota
	modeInsert
	modeCommand
	modeFilter
	modeVisual       // a message is selected for actions
	modeChatSearch   // typing a "/" search in the open chat
	modeGlobalSearch // typing a search across all chats
)

// Model is the root Bubble Tea model.
type Model struct {
	commands chan<- messages.Command
	sidebarW int
	bgSeq    string // SGR sequence painting the theme background; empty to keep the terminal's

	width, height int
	screen        screen
	focus         pane
	mode          mode
	pendingG      bool // first "g" of "gg" was pressed
	showHelp      bool
	helpScroll    int

	chats      []*messages.Conversation // chats with messages, newest first
	allChats   []*messages.Conversation // every known chat and contact (forward targets)
	archive    bool                     // showing archived chats instead of the inbox
	unreadOnly bool                     // list shows only chats with unread messages
	filter     string
	cursor     int // index into the list items (see itemAt)
	listOffset int

	current  *messages.Conversation // chat open on the right, nil if none
	msgs     []messages.Message
	msgSpans []msgSpan // content lines of each message, for lazy image loading
	msgLines []string  // the rendered message lines (for clicking links)

	// unread messages when the chat was opened: shown with a divider
	unreadFor   string // chat JID
	unreadCount int
	unreadID    string // first unread message, found once messages load
	focused     bool   // the terminal window has focus (unfocused: nothing counts as seen)
	vp          viewport.Model

	img *images // nil-safe; nil disables images and avatars

	attachments []*attachment // files and pasted images waiting to be sent
	lastPickDir string        // where yazi opens next time
	sender      Sender
	clip        Clipboard

	actions Actions
	sel     int               // selected message (visual mode), index into msgs
	picker  bool              // reaction picker open
	replyTo *messages.Message // message being replied to
	info    *infoState        // chat info panel, nil when closed

	search   *chatSearch // active in-chat search, nil when none
	searcher Searcher

	global         *globalSearch // search-all-messages screen, nil when closed
	globalSearcher GlobalSearcher

	fwd       *forwardPicker // "Forward to…" screen, nil when closed
	forwarder Forwarder

	stk      *stickerPicker // sticker/GIF tray, nil when closed
	stickers StickerSender

	view     *mediaView     // full-screen photo/sticker/GIF viewer, nil when closed
	confirm  *confirmDelete // waiting for enter to delete, nil otherwise
	deleter  Deleter
	pic      *pictureView // full-screen profile picture, nil when closed
	pictures PictureSource

	mentioner Mentioner
	mention   *mentionPicker                    // "@name" picker, nil when closed
	members   map[string][]messages.GroupMember // per group; nil entry = loading
	chosen    []chosenMention                   // members picked into the message being written

	privacy         Privacy
	privacyChecked  bool
	readReceiptsOff bool // your WhatsApp privacy setting; chats stop at delivered
	selfChat        bool // the open chat is your own (asked once on open, never while drawing)

	compose textarea.Model
	cmdline textinput.Model

	status    messages.SessionStatus
	notice    string
	noticeErr bool
	qr        string
	qrAttempt int
	qrTimeout int
}

// Options configures the UI.
type Options struct {
	SidebarWidth    int
	Theme           string // rose-pine, rose-pine-moon or rose-pine-dawn
	PaintBackground bool   // fill the screen with the theme's base colour

	Images termimg.Mode   // how to draw images (ModeOff disables them)
	Kitty  *termimg.Kitty // required for termimg.ModeKitty
	Media  MediaSource    // downloads media and profile pictures; may be nil

	Sender    Sender    // sends pasted images; nil disables sending them
	Clipboard Clipboard // nil uses the system clipboard
	Actions   Actions   // replies, reactions, downloads, chat info; may be nil
	Searcher  Searcher  // searches whole chat histories; may be nil

	GlobalSearcher GlobalSearcher // searches all chats; may be nil
	Forwarder      Forwarder      // forwards messages; may be nil
	Privacy        Privacy        // reads your privacy settings; may be nil
	Stickers       StickerSender  // sends stickers and GIFs; may be nil
	Mentioner      Mentioner      // group members for @mentions; may be nil
	Pictures       PictureSource  // full-size profile pictures; may be nil
	Deleter        Deleter        // deletes messages and chats; may be nil
}

// Privacy reads WhatsApp privacy settings; *messages.SessionManager
// implements it.
type Privacy interface {
	ReadReceiptsOff(ctx context.Context) (bool, error)
	IsSelfChat(jid string) bool
}

type privacyMsg struct {
	receiptsOff bool
	err         error
}

const composePlaceholder = "press i to type a message"

// New creates the UI model. Commands for the backend are sent on commands;
// initial seeds the chat list with cached conversations.
func New(commands chan<- messages.Command, initial []*messages.Conversation, opts Options) Model {
	ApplyTheme(opts.Theme)
	sidebarWidth := opts.SidebarWidth
	compose := newCompose()

	cmdline := textinput.New()
	cmdline.Prompt = ":"
	cmdline.PromptStyle = styleFilter
	cmdline.TextStyle = styleBase
	cmdline.Cursor.Style = lipgloss.NewStyle().Foreground(pal.Rose)
	cmdline.CharLimit = 0

	if sidebarWidth < 30 {
		sidebarWidth = 38
	}
	m := Model{
		commands: commands,
		sidebarW: sidebarWidth,
		bgSeq:    backgroundSeq(opts.PaintBackground),
		vp:       viewport.New(0, 0),
		compose:  compose,
		cmdline:  cmdline,
		img:      newImages(opts.Images, opts.Kitty, opts.Media),
		sender:   opts.Sender,
		clip:     opts.Clipboard,
		actions:  opts.Actions,
		searcher: opts.Searcher,

		globalSearcher: opts.GlobalSearcher,
		forwarder:      opts.Forwarder,
		privacy:        opts.Privacy,
		stickers:       opts.Stickers,
		mentioner:      opts.Mentioner,
		pictures:       opts.Pictures,
		deleter:        opts.Deleter,
		members:        map[string][]messages.GroupMember{},
		focused:        true, // until the terminal says otherwise
	}
	if m.clip == nil {
		m.clip = systemClipboard{}
	}
	m.vp.KeyMap = viewport.KeyMap{} // keys are handled by the model
	m.setChats(initial)
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// dispatch sends a command to the backend without blocking the UI loop.
func (m Model) dispatch(name string, params ...string) tea.Cmd {
	ch := m.commands
	return func() tea.Msg {
		ch <- messages.Command{Name: name, Params: params}
		return nil
	}
}

// sortChats orders chats by their latest message, newest first.
func sortChats(cs []*messages.Conversation) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].LastMsgTime != cs[j].LastMsgTime {
			return cs[i].LastMsgTime > cs[j].LastMsgTime
		}
		return chatName(cs[i]) < chatName(cs[j])
	})
}

// setChats replaces the chat list, keeping the cursor on the same chat.
func (m *Model) setChats(cs []*messages.Conversation) {
	var selected string
	if c := m.selectedChat(); c != nil {
		selected = c.JID
	}
	_, onArchiveRow := m.itemAt(m.cursor)
	onArchiveRow = onArchiveRow && m.chats != nil
	// Contacts you never messaged come without a timestamp; like WhatsApp,
	// only show actual conversations.
	list := make([]*messages.Conversation, 0, len(cs))
	m.allChats = m.allChats[:0]
	for _, c := range cs {
		if c == nil {
			continue
		}
		m.allChats = append(m.allChats, c)
		if c.LastMsgTime > 0 {
			list = append(list, c)
		}
	}
	sortChats(list)
	m.chats = list
	// keep the cursor where it was; start on the newest chat, not the
	// archive row (enter right after launch opens your latest chat)
	m.cursor = 0
	if m.hasArchiveRow() && !onArchiveRow {
		m.cursor = 1
	}
	for i := 0; i < m.listLen(); i++ {
		if c, _ := m.itemAt(i); c != nil && c.JID == selected {
			m.cursor = i
			break
		}
	}
	if m.current != nil {
		for _, c := range m.chats {
			if c.JID == m.current.JID {
				m.current = c
				break
			}
		}
	}
	m.clampCursor()
}

// visibleChats is the current list (inbox or archive), filtered.
func (m Model) visibleChats() []*messages.Conversation {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	out := make([]*messages.Conversation, 0, len(m.chats))
	for _, c := range m.chats {
		if c.IsArchived != m.archive || (m.unreadOnly && c.Unread == 0) {
			continue
		}
		if q != "" && !filterMatches(c, q) {
			continue
		}
		out = append(out, c)
	}
	// While filtering, also offer contacts you haven't chatted with yet
	// (archived chats too), so you can start a chat with anyone.
	if q != "" && !m.archive && !m.unreadOnly {
		seen := make(map[string]bool, len(out))
		for _, c := range out {
			seen[c.JID] = true
		}
		n := 0
		for _, c := range m.allChats {
			if seen[c.JID] || n >= maxContactResults || !filterMatches(c, q) {
				continue
			}
			if c.LastMsgTime > 0 && !c.IsArchived {
				continue // already listed above
			}
			out = append(out, c)
			n++
		}
	}
	return out
}

// maxContactResults caps extra contacts shown while filtering.
const maxContactResults = 30

// filterMatches: the name contains q, or q's digits are in the number.
func filterMatches(c *messages.Conversation, q string) bool {
	if strings.Contains(strings.ToLower(chatName(c)), q) {
		return true
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, q)
	return len(digits) >= 4 && len(digits) == len(strings.ReplaceAll(strings.ReplaceAll(q, " ", ""), "+", "")) &&
		strings.Contains(strings.Split(c.JID, "@")[0], digits)
}

// chatCounts returns how many chats are in the inbox and the archive, and
// how many chats in the current list have unread messages.
func (m Model) chatCounts() (inbox, archived, unread int) {
	for _, c := range m.chats {
		if c.IsArchived {
			archived++
		} else {
			inbox++
		}
		if c.IsArchived == m.archive && c.Unread > 0 {
			unread++
		}
	}
	return inbox, archived, unread
}

// toggleArchive switches between the inbox and the archived chats.
func (m *Model) toggleArchive() {
	m.archive = !m.archive
	m.cursor, m.listOffset = 0, 0
	m.focus = paneList
	m.clampCursor()
}

// hasArchiveRow: the inbox starts with an "Archived" row, like WhatsApp.
func (m Model) hasArchiveRow() bool {
	if m.archive || m.filter != "" || m.unreadOnly {
		return false
	}
	_, archived, _ := m.chatCounts()
	return archived > 0
}

// listLen is the number of list items (chats, plus the archive row).
func (m Model) listLen() int {
	n := len(m.visibleChats())
	if m.hasArchiveRow() {
		n++
	}
	return n
}

// itemAt returns list item i: a chat, or (nil, true) for the archive row.
func (m Model) itemAt(i int) (c *messages.Conversation, archiveRow bool) {
	if m.hasArchiveRow() {
		if i == 0 {
			return nil, true
		}
		i--
	}
	if v := m.visibleChats(); i >= 0 && i < len(v) {
		return v[i], false
	}
	return nil, false
}

func (m Model) selectedChat() *messages.Conversation {
	c, _ := m.itemAt(m.cursor)
	return c
}

// openSelected opens the chat under the cursor, or the archive.
func (m *Model) openSelected() tea.Cmd {
	c, archiveRow := m.itemAt(m.cursor)
	if archiveRow {
		m.toggleArchive()
		return nil
	}
	return m.openChat(c)
}

func (m *Model) clampCursor() {
	n := m.listLen()
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	rows := m.listRows()
	if rows < 1 {
		rows = 1
	}
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	if m.cursor >= m.listOffset+rows {
		m.listOffset = m.cursor - rows + 1
	}
	if m.listOffset < 0 {
		m.listOffset = 0
	}
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
}

// openChat shows the given chat on the right and asks the backend for it.
func (m *Model) openChat(c *messages.Conversation) tea.Cmd {
	if c == nil {
		return nil
	}
	m.screen = screenChat
	m.focus = paneMessages
	if m.current == nil || m.current.JID != c.JID {
		m.current = c
		m.selfChat = m.privacy != nil && m.privacy.IsSelfChat(c.JID)
		m.msgs = nil
		m.compose.SetValue("")
		m.replyTo = nil
		m.attachments = nil
		m.mention, m.chosen = nil, nil
		m.search = nil
		m.compose.SetHeight(1)
	}
	// always open at the first unread message, or else the newest
	m.unreadFor, m.unreadCount, m.unreadID = "", 0, ""
	if c.Unread > 0 {
		m.unreadFor, m.unreadCount = c.JID, int(c.Unread)
	}
	if m.mode == modeVisual {
		m.mode = modeNormal
	}
	m.sel = len(m.msgs) - 1
	m.resize()
	m.refreshMessages(true)
	return tea.Batch(m.dispatch("select", c.JID), m.markSeen())
}

// markSeen marks the open chat read when you're looking at it: it's on
// screen and the terminal has focus. Called on open, when the chat list
// changes (a new message arrived) and when the window gets focus back.
func (m *Model) markSeen() tea.Cmd {
	if m.screen != screenChat || m.current == nil || !m.focused {
		return nil
	}
	jid, unseen := m.current.JID, false
	for _, list := range [][]*messages.Conversation{m.chats, m.allChats, {m.current}} {
		for _, c := range list {
			if c.JID == jid && (c.Unread > 0 || c.Mentioned) {
				c.Unread, c.Mentioned, unseen = 0, false, true // shown as read right away
			}
		}
	}
	if !unseen {
		return nil
	}
	return m.dispatch("read", jid)
}

// clearFilter ends a chat-list search, keeping the cursor on the same chat
// when it's still listed.
func (m *Model) clearFilter() {
	c := m.selectedChat()
	m.filter = ""
	m.cursor = 0
	if m.hasArchiveRow() {
		m.cursor = 1
	}
	for i := 0; c != nil && i < m.listLen(); i++ {
		if x, _ := m.itemAt(i); x != nil && x.JID == c.JID {
			m.cursor = i
			break
		}
	}
	m.clampCursor()
}

func (m *Model) back() {
	m.screen = screenList
	m.focus = paneList
	m.mode = modeNormal
	m.compose.Blur()
	m.clampCursor()
}

// Update handles all events, then starts loading any images that became
// visible.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm := next.(Model)
	if load := nm.loadVisible(); load != nil {
		return nm, tea.Batch(cmd, load)
	}
	return nm, cmd
}

// loadVisible returns commands loading avatars and media on screen (and one
// screen above and below, so scrolling finds them ready).
func (m Model) loadVisible() tea.Cmd {
	if m.img != nil && m.width > 0 && m.stk != nil {
		var cmds []tea.Cmd
		for _, l := range m.stickerLoads() {
			meta, _ := l.msg.MediaMeta()
			if c := m.img.ensureMessage(l.msg, meta, l.cols, l.rows); c != nil {
				cmds = append(cmds, c)
			}
		}
		return tea.Batch(cmds...)
	}
	if m.img == nil || m.width == 0 || m.qr != "" || m.showHelp || m.info != nil || m.global != nil || m.fwd != nil || m.pic != nil || m.view != nil {
		return nil
	}
	var cmds []tea.Cmd
	add := func(c tea.Cmd) {
		if c != nil {
			cmds = append(cmds, c)
		}
	}
	for i := m.listOffset; i < m.listLen() && i < m.listOffset+m.listRows(); i++ {
		if c, _ := m.itemAt(i); c != nil {
			add(m.img.ensureAvatar(c, avatarBigCols, avatarBigRows))
		}
	}
	if m.screen == screenChat && m.current != nil {
		add(m.img.ensureAvatar(m.current, avatarSmallCols, avatarSmallRows))
		top, bottom := m.vp.YOffset-m.vp.Height, m.vp.YOffset+2*m.vp.Height
		maxInner := m.bubbleMaxInner(m.rightWidth())
		for _, sp := range m.msgSpans {
			if sp.end < top || sp.start > bottom {
				continue
			}
			msg := m.msgs[sp.idx]
			if meta, ok := msg.MediaMeta(); ok {
				cols, rows := m.mediaCells(meta, maxInner)
				add(m.img.ensureMessage(msg, meta, cols, rows))
			}
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pasteImageMsg:
		if m.screen == screenChat && m.current != nil {
			m.addAttachments(msg.att)
			m.notice = ""
		}
		return m, nil
	case pictureMsg:
		return m.applyPicture(msg)
	case mediaViewMsg:
		return m.applyMediaView(msg)
	case chatDeletedMsg:
		return m.applyChatDeleted(msg)
	case videoReadyMsg:
		return m.startVideo(msg)
	case videoDoneMsg:
		m.img.reset() // the player drew over the screen
		m.refreshMessages(false)
		if msg.err != nil {
			m.notice, m.noticeErr = "video: "+msg.err.Error(), true
		}
		return m, nil
	case membersMsg:
		if msg.err != nil {
			m.members[msg.chat] = []messages.GroupMember{} // don't retry in a loop
			m.notice, m.noticeErr = msg.err.Error(), true
		} else {
			m.members[msg.chat] = msg.members
		}
		m.resize()
		return m, nil
	case recentMediaMsg:
		if m.stk != nil {
			m.stk.loading, m.stk.err = false, msg.err
			m.stk.items = [2][]messages.Message{msg.stickers, msg.gifs}
		}
		return m, nil
	case pickedFilesMsg:
		// yazi drew its own images; ours may be gone, so draw them again
		m.img.reset()
		m.refreshMessages(false)
		if msg.err != nil {
			m.notice, m.noticeErr = msg.err.Error(), true
			return m, nil
		}
		if len(msg.paths) == 0 {
			return m, nil // nothing chosen
		}
		m.lastPickDir = filepath.Dir(msg.paths[0])
		if m.stk != nil { // "n" in the sticker tray: make stickers/GIFs
			tab := m.stk.newFor
			m.stk = nil
			m.notice, m.noticeErr = "Converting…", false
			return m, m.sendNewFromFiles(msg.paths, tab)
		}
		return m, m.loadAttachments(msg.paths)
	case attachmentsReadyMsg:
		if msg.err != nil {
			m.notice, m.noticeErr = msg.err.Error(), true
		}
		if len(msg.atts) > 0 && m.current != nil {
			m.addAttachments(msg.atts...)
			if msg.err == nil {
				m.notice = ""
			}
			m.mode = modeInsert
			return m, m.compose.Focus()
		}
		return m, nil
	case pasteTextMsg:
		if m.mode == modeInsert {
			m.insertText(string(msg))
		}
		return m, nil
	case pasteFailMsg:
		m.notice, m.noticeErr = "paste: "+msg.err.Error(), true
		return m, nil
	case sentMsg:
		switch {
		case msg.err != nil:
			m.notice, m.noticeErr = msg.err.Error(), true
		case msg.n > 1:
			m.notice, m.noticeErr = fmt.Sprintf("Sent %d files", msg.n), false
		default:
			m.notice, m.noticeErr = "Sent", false
		}
		return m, nil
	case actionDoneMsg:
		if msg.err != nil {
			m.notice, m.noticeErr = msg.err.Error(), true
		} else if msg.ok != "" {
			m.notice, m.noticeErr = msg.ok, false
		}
		return m, nil
	case privateChatMsg:
		return m.openPrivateChat(msg)
	case searchResultMsg:
		return m.applySearchResult(msg)
	case globalHitsMsg:
		return m.applyGlobalHits(msg)
	case openHitMsg:
		return m.showHit(msg)
	case infoMsg:
		if m.info != nil && m.info.conv.JID == msg.jid {
			m.info.loading, m.info.data, m.info.err, m.info.picture = false, msg.data, msg.err, msg.picture
		}
		return m, nil

	case imageReadyMsg:
		if m.img.apply(msg) && msg.key.kind == imgMessage && m.current != nil {
			m.refreshMessages(false)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.listOffset = 0 // recomputed for the new height, keeping the cursor visible
		m.resize()
		m.refreshMessages(false)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case chatListMsg:
		m.setChats(msg)
		return m, m.markSeen() // a message that arrived while you watch is read

	case tea.FocusMsg:
		m.focused = true
		return m, m.markSeen()

	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case screenMsg:
		if m.current == nil {
			return m, nil
		}
		if len(msg) > 0 && msg[0].ChatId != m.current.JID {
			return m, nil // stale response for a chat we already left
		}
		// Keep the reader's place when history arrives while scrolled up,
		// and the selection on the same message.
		follow := (len(m.msgs) == 0 || m.vp.AtBottom()) && m.mode != modeVisual
		var selID string
		if s, ok := m.selected(); ok {
			selID = s.Id
		}
		if m.search != nil && len(msg) < len(m.msgs) {
			return m, nil // keep the older messages a search loaded
		}
		anchor, delta := m.topVisible() // before m.msgs changes
		m.msgs = append([]messages.Message(nil), msg...)
		m.sel = len(m.msgs) - 1
		for i, x := range m.msgs {
			if x.Id == selID {
				m.sel = i
			}
		}
		if s := m.search; s != nil {
			m.findMatches()
			for i, idx := range s.matches {
				if idx == m.sel {
					s.pos = i
				}
			}
		}
		if m.unreadFor == m.current.JID && m.unreadID == "" && len(m.msgs) > 0 {
			// first load of a chat with unread messages: start at them
			m.unreadFor = ""
			if i := firstUnread(m.msgs, m.unreadCount); i >= 0 {
				m.unreadID = m.msgs[i].Id
				m.refreshMessages(false)
				m.scrollToUnread()
				return m, nil
			}
			follow = true // more unread than loaded: start at the newest
		}
		if follow {
			m.refreshMessages(true)
			return m, nil
		}
		// older history arrived above: keep the same messages on screen
		m.refreshMessages(false)
		m.restoreTop(anchor, delta)
		return m, nil

	case newMessageMsg:
		if m.current != nil && msg.ChatId == m.current.JID {
			atBottom := m.vp.AtBottom()
			m.msgs = append(m.msgs, messages.Message(msg))
			m.refreshMessages(atBottom)
		}
		return m, nil

	case statusMsg:
		m.status = messages.SessionStatus(msg)
		if m.status.Connected {
			m.qr = ""
			if !m.privacyChecked && m.privacy != nil {
				m.privacyChecked = true
				p := m.privacy
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					off, err := p.ReadReceiptsOff(ctx)
					return privacyMsg{receiptsOff: off, err: err}
				}
			}
		}
		return m, nil
	case privacyMsg:
		if msg.err != nil {
			m.privacyChecked = false // try again on the next connect
		} else {
			m.readReceiptsOff = msg.receiptsOff
		}
		return m, nil

	case qrMsg:
		m.qr, m.qrAttempt, m.qrTimeout = msg.qr, msg.attempt, msg.timeout
		return m, nil

	case noticeMsg:
		m.notice, m.noticeErr = msg.text, msg.isErr
		if msg.text == "Successfully logged in!" || msg.text == "Session restored successfully" {
			m.qr = ""
		}
		return m, nil

	case clearMsg:
		m.notice = ""
		return m, nil

	case helpMsg:
		m.showHelp = true
		return m, nil

	case quitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	if m.confirm != nil {
		return m.handleConfirm(msg)
	}
	if m.view != nil {
		return m.handleMediaView(msg)
	}
	if m.pic != nil {
		return m.handlePicture(msg)
	}
	if m.stk != nil {
		return m.handleStickers(msg)
	}
	if m.fwd != nil {
		return m.handleForward(msg)
	}
	if m.global != nil {
		return m.handleGlobalSearch(msg)
	}
	// files dragged onto the window arrive as a paste of their paths
	if msg.Paste && !m.picker && (m.mode == modeNormal || m.mode == modeVisual || m.mode == modeInsert) {
		if paths, ok := droppedFiles(string(msg.Runes)); ok {
			if m.screen != screenChat || m.current == nil {
				m.notice, m.noticeErr = "open a chat first, then drop the files on it", true
				return m, nil
			}
			m.notice, m.noticeErr = fmt.Sprintf("Attaching %d file(s)…", len(paths)), false
			return m, m.loadAttachments(paths)
		}
	}
	// ctrl+x drops the pasted image, else cancels the reply, in any mode
	// that shows them (not while typing a command or search).
	if key == "ctrl+x" && (len(m.attachments) > 0 || m.replyTo != nil) &&
		(m.mode == modeNormal || m.mode == modeVisual || m.mode == modeInsert) && !m.picker {
		if len(m.attachments) > 0 {
			m.dropAttachment()
		} else {
			m.cancelReply()
		}
		return m, nil
	}
	switch m.mode {
	case modeInsert:
		return m.handleInsert(msg)
	case modeCommand:
		return m.handleCommand(msg)
	case modeFilter:
		return m.handleFilter(msg)
	case modeVisual:
		return m.handleVisual(msg)
	case modeChatSearch:
		return m.handleChatSearch(msg)
	}
	if m.info != nil {
		return m.handleInfo(msg)
	}

	if m.showHelp {
		switch key {
		case "?", "esc", "q", "backspace":
			m.showHelp, m.helpScroll = false, 0
		case "j", "down":
			m.helpScroll = min(m.helpScroll+1, m.helpMaxScroll())
		case "k", "up":
			m.helpScroll = max(m.helpScroll-1, 0)
		case "ctrl+d", "pgdown", "space":
			m.helpScroll = min(m.helpScroll+m.mainHeight()/2, m.helpMaxScroll())
		case "ctrl+u", "pgup":
			m.helpScroll = max(m.helpScroll-m.mainHeight()/2, 0)
		case "G":
			m.helpScroll = m.helpMaxScroll()
		case "g":
			m.helpScroll = 0
		}
		return m, nil
	}

	// "gg" goes to the top; any other key cancels a pending "g".
	if m.pendingG {
		m.pendingG = false
		if key == "g" {
			if m.focus == paneMessages && m.screen == screenChat {
				m.vp.GotoTop()
			} else {
				m.cursor = 0
				m.clampCursor()
			}
			return m, nil
		}
	}

	switch key {
	case ":":
		m.mode = modeCommand
		m.cmdline.SetValue("")
		return m, m.cmdline.Focus()
	case "?":
		m.showHelp = true
		return m, nil
	case "g":
		m.pendingG = true
		return m, nil
	case "esc":
		m.notice = ""
		if m.search != nil { // like :noh
			m.search = nil
			m.refreshMessages(false)
		} else if m.filter != "" && m.focus == paneList {
			m.clearFilter()
		}
		return m, nil
	}

	if m.screen == screenChat && m.focus == paneMessages {
		return m.handleMessagesPane(key)
	}
	return m.handleListPane(key)
}

func (m Model) handleListPane(key string) (tea.Model, tea.Cmd) {
	half := m.listRows() / 2
	if half < 1 {
		half = 1
	}
	switch key {
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "ctrl+d":
		m.moveCursor(half)
	case "ctrl+u":
		m.moveCursor(-half)
	case "G":
		m.cursor = m.listLen() - 1
		m.clampCursor()
	case "enter", "l", "right":
		return m, m.openSelected()
	case "A":
		m.toggleArchive()
	case "S":
		return m, m.openGlobalSearch()
	case "u":
		m.unreadOnly = !m.unreadOnly
		m.cursor, m.listOffset = 0, 0
		m.clampCursor()
	case "K":
		return m, m.openInfo(m.selectedChat())
	case "V":
		return m, m.openPicture(m.selectedChat())
	case "d":
		m.askDeleteChat(m.selectedChat())
	case "/":
		m.mode = modeFilter
		m.cmdline.Prompt = "/"
		m.cmdline.SetValue(m.filter)
		m.cmdline.CursorEnd()
		return m, m.cmdline.Focus()
	case "backspace", "q":
		if m.screen == screenChat {
			m.back()
			return m, nil
		}
		if m.filter != "" {
			m.clearFilter()
			return m, nil
		}
		if m.unreadOnly {
			m.unreadOnly = false
			m.clampCursor()
			return m, nil
		}
		if m.archive {
			m.toggleArchive()
			return m, nil
		}
		if key == "q" {
			return m, tea.Quit
		}
	case "tab", "ctrl+l":
		if m.screen == screenChat {
			m.focus = paneMessages
		}
	}
	return m, nil
}

func (m Model) handleMessagesPane(key string) (tea.Model, tea.Cmd) {
	half := m.vp.Height / 2
	if half < 1 {
		half = 1
	}
	switch key {
	case "j", "down":
		m.vp.LineDown(1)
	case "k", "up":
		m.vp.LineUp(1)
	case "ctrl+d":
		m.vp.LineDown(half)
	case "ctrl+u":
		m.vp.LineUp(half)
	case "ctrl+f", "pgdown":
		m.vp.ViewDown()
	case "ctrl+b", "pgup":
		m.vp.ViewUp()
	case "G":
		m.vp.GotoBottom()
	case "h", "left", "tab", "ctrl+h":
		m.focus = paneList
		m.clampCursor()
	case "i", "enter":
		m.mode = modeInsert
		return m, m.compose.Focus()
	case "ctrl+v", "p", "P":
		m.mode = modeInsert
		return m, tea.Batch(m.compose.Focus(), m.paste())
	case "a": // attach
		return m, m.pickFiles()
	case "s": // stickers & GIFs
		return m, m.openStickers()
	case "v":
		m.enterVisual()
	case "/":
		return m, m.startSearch()
	case "S":
		return m, m.openGlobalSearch()
	case "@":
		m.nextMention()
	case "A":
		m.openArchiveFromChat()
	case "n":
		m.nextMatch(-1)
	case "N":
		m.nextMatch(1)
	case "K":
		return m, m.openInfo(m.current)
	case "backspace", "q":
		m.back()
	}
	return m, nil
}

func (m Model) handleInsert(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mention != nil && m.handleMentionKey(msg.String()) {
		m.resize()
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.compose.Blur()
		return m, nil
	case "ctrl+v":
		return m, m.paste()
	case "ctrl+x":
		if len(m.attachments) > 0 {
			m.dropAttachment()
		} else if m.replyTo != nil {
			m.cancelReply()
		}
		return m, nil
	case "ctrl+a":
		return m, m.pickFiles()
	case "enter":
		text := strings.TrimSpace(m.compose.Value())
		if m.current == nil {
			return m, nil
		}
		if m.replyTo != nil && len(m.attachments) == 0 {
			if text == "" {
				return m, nil
			}
			if m.actions == nil {
				m.notice, m.noticeErr = "replies aren't available", true
				return m, nil
			}
			text, jids := mentionsForSend(text, m.chosen)
			a, chat, quoted := m.actions, m.current.JID, *m.replyTo
			m.compose.SetValue("")
			m.chosen, m.mention = nil, nil
			m.cancelReply()
			m.fitCompose()
			m.vp.GotoBottom()
			return m, m.action("", func(ctx context.Context) (string, error) {
				return "", a.SendReply(ctx, chat, text, quoted, jids)
			})
		}
		if n := len(m.attachments); n > 0 {
			if m.sender == nil {
				m.notice, m.noticeErr = "sending files isn't available", true
				return m, nil
			}
			cmd := m.sendAttachments(text)
			m.compose.SetValue("")
			m.fitCompose()
			m.clearAttachments()
			m.notice, m.noticeErr = "Sending…", false
			if n > 1 {
				m.notice = fmt.Sprintf("Sending %d files…", n)
			}
			return m, cmd
		}
		if text == "" {
			return m, nil
		}
		text, jids := mentionsForSend(text, m.chosen)
		m.compose.SetValue("")
		m.chosen, m.mention = nil, nil
		m.fitCompose()
		m.vp.GotoBottom()
		if len(jids) > 0 && m.mentioner != nil {
			mn, chat := m.mentioner, m.current.JID
			return m, m.action("", func(ctx context.Context) (string, error) {
				return "", mn.SendText(ctx, chat, text, jids)
			})
		}
		return m, m.dispatch("send", m.current.JID, text)
	}
	var cmd tea.Cmd
	m.growCompose()
	m.compose, cmd = m.compose.Update(msg)
	m.fitCompose()
	load := m.updateMentions()
	m.resize()
	return m, tea.Batch(cmd, load)
}

func (m Model) handleCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.cmdline.Blur()
		return m, nil
	case "backspace":
		if m.cmdline.Value() == "" {
			m.mode = modeNormal
			m.cmdline.Blur()
			return m, nil
		}
	case "enter":
		line := strings.TrimSpace(m.cmdline.Value())
		m.mode = modeNormal
		m.cmdline.Blur()
		return m.runCommand(line)
	}
	var cmd tea.Cmd
	m.cmdline, cmd = m.cmdline.Update(msg)
	return m, cmd
}

// runCommand executes an ex-style command. UI commands are handled here;
// anything else is passed to the backend command registry.
func (m Model) runCommand(line string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return m, nil
	}
	switch fields[0] {
	case "q", "q!", "qa", "quit", "wq", "x":
		return m, tea.Quit
	case "h", "help":
		m.showHelp = true
		return m, nil
	case "b", "back":
		m.back()
		return m, nil
	case "info":
		c := m.current
		if m.screen == screenList || c == nil {
			c = m.selectedChat()
		}
		return m, m.openInfo(c)
	case "sticker", "gif":
		if m.screen != screenChat || m.current == nil {
			m.notice, m.noticeErr = "open a chat first", true
			return m, nil
		}
		tab := tabStickers
		if fields[0] == "gif" {
			tab = tabGIFs
		}
		if len(fields) == 1 {
			cmd := m.openStickers()
			if m.stk != nil {
				m.stk.tab = tab
			}
			return m, cmd
		}
		if m.stickers == nil {
			return m, nil
		}
		m.notice, m.noticeErr = "Converting…", false
		return m, m.sendNewFromFiles([]string{config.ExpandPath(strings.Join(fields[1:], " "))}, tab)
	case "attach", "att":
		if m.screen != screenChat || m.current == nil {
			m.notice, m.noticeErr = "open a chat first", true
			return m, nil
		}
		if len(fields) == 1 {
			return m, m.pickFiles()
		}
		// one path with spaces, or several paths
		paths := fields[1:]
		if joined := strings.Join(fields[1:], " "); fileExists(config.ExpandPath(joined)) {
			paths = []string{joined}
		}
		return m, m.loadAttachments(paths)
	case "download-dir", "downloads", "dl":
		if len(fields) == 1 {
			m.notice, m.noticeErr = "Downloads go to "+tildePath(downloadDir())+"  (change: :download-dir <path>)", false
			return m, nil
		}
		dir := config.ExpandPath(strings.Join(fields[1:], " "))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			m.notice, m.noticeErr = err.Error(), true
			return m, nil
		}
		if err := config.SetDownloadPath(dir); err != nil {
			m.notice, m.noticeErr = err.Error(), true
			return m, nil
		}
		m.notice, m.noticeErr = "Downloads now go to "+tildePath(dir), false
		return m, nil
	case "search", "s":
		cmd := m.openGlobalSearch()
		if m.global != nil && len(fields) > 1 {
			m.global.query = strings.Join(fields[1:], " ")
			m.cmdline.SetValue(m.global.query)
			m.cmdline.CursorEnd()
			return m, tea.Batch(cmd, m.runGlobalSearch())
		}
		return m, cmd
	case "unread":
		m.unreadOnly = !m.unreadOnly
		m.screen, m.focus = screenList, paneList
		m.cursor, m.listOffset = 0, 0
		m.clampCursor()
		return m, nil
	case "archive", "archived":
		if !m.archive {
			m.toggleArchive()
		}
		m.screen = screenList
		return m, nil
	case "inbox", "chats":
		if m.archive {
			m.toggleArchive()
		}
		m.screen = screenList
		return m, nil
	}
	return m, m.dispatch(fields[0], fields[1:]...)
}

func (m Model) handleFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter = ""
		m.mode = modeNormal
		m.cmdline.Blur()
		m.cmdline.Prompt = ":"
		m.clampCursor()
		return m, nil
	case "enter":
		// stop typing, keep the results: browse them with the usual keys
		m.mode = modeNormal
		m.cmdline.Blur()
		m.cmdline.Prompt = ":"
		m.filter = strings.TrimSpace(m.filter)
		m.clampCursor()
		return m, nil
	case "down", "ctrl+n", "ctrl+j":
		m.moveCursor(1)
		return m, nil
	case "up", "ctrl+p", "ctrl+k":
		m.moveCursor(-1)
		return m, nil
	}
	var cmd tea.Cmd
	m.cmdline, cmd = m.cmdline.Update(msg)
	if m.cmdline.Value() != m.filter {
		m.filter = m.cmdline.Value()
		m.cursor = 0
		m.listOffset = 0
		m.clampCursor()
	}
	return m, cmd
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// firstUnread finds where the last n incoming messages start.
func firstUnread(msgs []messages.Message, n int) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if !msgs[i].FromMe {
			n--
			if n == 0 {
				return i
			}
		}
	}
	return -1 // more unread than loaded: no telling where they start
}

// topVisible returns the message at the top of the view, and how many lines
// of it are scrolled past.
func (m Model) topVisible() (id string, delta int) {
	y := m.vp.YOffset
	for _, sp := range m.msgSpans {
		if sp.idx < len(m.msgs) && sp.start <= y {
			id, delta = m.msgs[sp.idx].Id, y-sp.start
		}
	}
	return id, delta
}

// restoreTop scrolls back to the message topVisible returned.
func (m *Model) restoreTop(id string, delta int) {
	for _, sp := range m.msgSpans {
		if sp.idx < len(m.msgs) && m.msgs[sp.idx].Id == id {
			m.vp.SetYOffset(sp.start + delta)
			return
		}
	}
}

// scrollToUnread puts the unread divider at the top of the view.
func (m *Model) scrollToUnread() {
	for _, sp := range m.msgSpans {
		if m.msgs[sp.idx].Id == m.unreadID {
			m.vp.SetYOffset(max(sp.start-2, 0))
			return
		}
	}
}

// nextMention selects the next older message that mentions you, wrapping.
func (m *Model) nextMention() {
	var idx []int
	for i, msg := range m.msgs {
		if !msg.FromMe && mentionsYou(msg) {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		m.notice, m.noticeErr = "no messages here mention you", true
		return
	}
	from := len(m.msgs)
	if m.mode == modeVisual {
		from = m.sel
	}
	next := idx[len(idx)-1] // wrap to the newest
	for i := len(idx) - 1; i >= 0; i-- {
		if idx[i] < from {
			next = idx[i]
			break
		}
	}
	m.mode, m.sel = modeVisual, next
	m.refreshMessages(false)
	m.scrollToSelection()
}

// openArchiveFromChat shows the archived chats in the sidebar (or goes
// back to the inbox) from inside a chat.
func (m *Model) openArchiveFromChat() {
	if m.mode == modeVisual {
		m.exitVisual()
	}
	m.toggleArchive() // also focuses the list
}
