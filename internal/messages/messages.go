// this package manages the messages
package messages

type UiMessageHandler interface {
	NewMessage(Message)
	NewScreen([]Message)
	SetChats([]Chat)
	UpdateChatList([]*Conversation)
	PrintError(error)
	PrintText(string)
	PrintFile(string)
	PrintQR(string)
	SetStatus(SessionStatus)
	OpenFile(string)
	ShowColorList()
	Clear()
	UpdateQR(qr string, attempt int, timeout int)
	PrintCommands()
	PrintHelp()
	Quit()
}

// data struct for current session status
type SessionStatus struct {
	BatteryCharge    int
	BatteryLoading   bool
	BatteryPowersave bool
	Connected        bool
	LastSeen         string
}

// message struct for battery messages
type BatteryMsg struct {
	charge    int
	loading   bool
	powersave bool
}

// message struct for status messages
type StatusMsg struct {
	connected bool
	err       error
}

// message object for commands
type Command struct {
	Name   string
	Params []string
}

// internal message representation to abstract from message lib
type Message struct {
	Id           string
	ChatId       string // the source of the message (group id or contact id)
	ContactId    string
	ContactName  string
	ContactShort string
	Timestamp    uint64
	FromMe       bool
	Forwarded    bool
	Text         string
	MediaType    string // "", "image", "sticker", "gif", "video", "document" or "audio"
	Media        []byte // marshalled waE2E.Message holding the media (see media.go)

	// Set when this message replies to another one.
	QuotedID     string
	QuotedSender string // JID of the quoted message's author
	QuotedText   string

	Reactions []Reaction // filled in when loading a chat

	Status int // delivery state of your own messages (Status* constants)

	Mentions map[string]string // "@<number>" in Text -> display name; filled when loading
}

// Reaction is one person's emoji reaction to a message.
type Reaction struct {
	Sender string // JID, or "" for yourself
	Emoji  string
}

// internal contact representation to abstract from message lib
type Chat struct {
	Id          string
	IsGroup     bool
	Name        string
	Unread      int
	LastMessage int64
}

type Contact struct {
	Id    string
	Name  string
	Short string
}

const GROUPSUFFIX = "@g.us"
const CONTACTSUFFIX = "@s.whatsapp.net"
const STATUSSUFFIX = "status@broadcast"

// Conversation represents a lightweight chat metadata for the list view
type Conversation struct {
	JID         string
	Name        string
	LastMsgTime int64
	Preview     string
	Unread      uint16
	IsPinned    bool
	IsArchived  bool
	Mentioned   bool // an unread message mentions you
	Index       int  // Heap index for internal use
}
