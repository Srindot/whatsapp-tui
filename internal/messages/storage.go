package messages

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Srindot/whatsapp-tui/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

// MessageDatabase stores messages and contact data in SQLite
type MessageDatabase struct {
	db *sql.DB
}

// Init initializes the message database with a file-based SQLite connection.
func (md *MessageDatabase) Init() error {
	dbPath := config.GetSessionFilePath() + "_meta.db"
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open metadata db: %w", err)
	}
	return md.InitWithDB(db)
}

// InitWithDB initializes the database schema using the provided *sql.DB.
// This allows tests to inject an in-memory SQLite instance.
func (md *MessageDatabase) InitWithDB(db *sql.DB) error {
	md.db = db

	_, err := md.db.Exec(`
	CREATE TABLE IF NOT EXISTS conversations (
		jid TEXT PRIMARY KEY,
		name TEXT,
		last_msg_time INTEGER,
		preview TEXT,
		unread INTEGER,
		is_pinned BOOLEAN
	);
	`)
	if err != nil {
		return fmt.Errorf("failed to create conversations table: %w", err)
	}

	// Backward-compatible migration: add is_archived column for existing databases.
	// SQLite ignores the ALTER if the column already exists when using this pattern.
	md.db.Exec(`ALTER TABLE conversations ADD COLUMN is_archived BOOLEAN DEFAULT 0`)
	md.db.Exec(`ALTER TABLE conversations ADD COLUMN mentioned BOOLEAN DEFAULT 0`)

	_, err = md.db.Exec(`
	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		chat_id TEXT,
		contact_id TEXT,
		contact_name TEXT,
		contact_short TEXT,
		timestamp INTEGER,
		from_me BOOLEAN,
		forwarded BOOLEAN,
		text TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_messages_chat_id ON messages(chat_id);
	CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages(chat_id, timestamp);
	`)
	if err != nil {
		return fmt.Errorf("failed to create messages table: %w", err)
	}

	// Media columns (added later): media_type is "image", "sticker", "gif" or
	// "video"; media holds the marshalled waE2E.Message needed to download it.
	// The ALTERs fail harmlessly when the columns already exist.
	md.db.Exec(`ALTER TABLE messages ADD COLUMN media_type TEXT DEFAULT ''`)
	md.db.Exec(`ALTER TABLE messages ADD COLUMN media BLOB`)
	md.db.Exec(`ALTER TABLE messages ADD COLUMN quoted_id TEXT DEFAULT ''`)
	md.db.Exec(`ALTER TABLE messages ADD COLUMN quoted_sender TEXT DEFAULT ''`)
	md.db.Exec(`ALTER TABLE messages ADD COLUMN quoted_text TEXT DEFAULT ''`)
	md.db.Exec(`ALTER TABLE messages ADD COLUMN status INTEGER DEFAULT 0`)

	// One reaction per person per message; an empty emoji removes it.
	if _, err := md.db.Exec(`
	CREATE TABLE IF NOT EXISTS reactions (
		msg_id TEXT,
		sender TEXT,
		emoji TEXT,
		timestamp INTEGER,
		PRIMARY KEY (msg_id, sender)
	)`); err != nil {
		return fmt.Errorf("failed to create reactions table: %w", err)
	}

	return nil
}

// msgColumns is the column list scanned by collectMessages.
const msgColumns = "id, chat_id, contact_id, contact_name, contact_short, timestamp, from_me, forwarded, text, " +
	"COALESCE(media_type, ''), media, COALESCE(quoted_id, ''), COALESCE(quoted_sender, ''), COALESCE(quoted_text, ''), " +
	"COALESCE(status, 0)"

// escapeLike escapes the SQL LIKE metacharacters (%, _, \) so they are
// treated as literal characters in a LIKE ? ESCAPE '\' clause.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// Close closes the underlying SQLite connection.
// Safe to call on nil or already-closed databases.
func (md *MessageDatabase) Close() error {
	if md.db != nil {
		return md.db.Close()
	}
	return nil
}

// AddMessage persists a message to SQLite.
// Returns nil on success, or an error if the insert fails.
func (md *MessageDatabase) AddMessage(msg Message) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	// Existing rows are kept, except that media info is filled in when it was
	// missing (e.g. messages stored before media support, re-fetched later).
	_, err := md.db.Exec(`
	INSERT INTO messages
	(id, chat_id, contact_id, contact_name, contact_short, timestamp, from_me, forwarded, text, media_type, media,
	 quoted_id, quoted_sender, quoted_text, status)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		status = MAX(COALESCE(messages.status, 0), excluded.status),
		media_type = CASE WHEN messages.media IS NULL AND excluded.media IS NOT NULL
			THEN excluded.media_type ELSE messages.media_type END,
		media = COALESCE(messages.media, excluded.media),
		quoted_id = CASE WHEN COALESCE(messages.quoted_id, '') = '' THEN excluded.quoted_id ELSE messages.quoted_id END,
		quoted_sender = CASE WHEN COALESCE(messages.quoted_id, '') = '' THEN excluded.quoted_sender ELSE messages.quoted_sender END,
		quoted_text = CASE WHEN COALESCE(messages.quoted_id, '') = '' THEN excluded.quoted_text ELSE messages.quoted_text END
	`,
		msg.Id, msg.ChatId, msg.ContactId, msg.ContactName,
		msg.ContactShort, msg.Timestamp, msg.FromMe, msg.Forwarded, msg.Text,
		msg.MediaType, nullBytes(msg.Media), msg.QuotedID, msg.QuotedSender, msg.QuotedText, msg.Status,
	)
	return err
}

// UpsertConversation updates or inserts a conversation
func (md *MessageDatabase) UpsertConversation(c Conversation) error {
	_, err := md.db.Exec(`
	INSERT INTO conversations (jid, name, last_msg_time, preview, unread, is_pinned, is_archived, mentioned)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(jid) DO UPDATE SET
		name=excluded.name,
		last_msg_time=excluded.last_msg_time,
		preview=excluded.preview,
		unread=excluded.unread,
		is_pinned=excluded.is_pinned,
		is_archived=excluded.is_archived,
		mentioned=excluded.mentioned;
	`, c.JID, c.Name, c.LastMsgTime, c.Preview, c.Unread, c.IsPinned, c.IsArchived, c.Mentioned)
	return err
}

// GetConversations retrieves all conversations from the DB
func (md *MessageDatabase) GetConversations() ([]Conversation, error) {
	rows, err := md.db.Query("SELECT jid, name, last_msg_time, preview, unread, is_pinned, is_archived, COALESCE(mentioned, 0) FROM conversations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.JID, &c.Name, &c.LastMsgTime, &c.Preview, &c.Unread, &c.IsPinned, &c.IsArchived, &c.Mentioned); err != nil {
			return nil, err
		}
		convs = append(convs, c)
	}
	return convs, nil
}

// GetMessages retrieves all messages for a chat.
// Returns the messages and any error encountered during the query.
func (md *MessageDatabase) GetMessages(chatId string) ([]Message, error) {
	if md.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := md.db.Query(`SELECT `+msgColumns+` FROM messages WHERE chat_id = ? ORDER BY timestamp ASC`, chatId)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	return collectMessages(rows)
}

// SearchMessages searches for messages containing the keyword.
// If chatId is non-empty, results are scoped to that chat; otherwise all chats are searched.
// Results are returned newest-first, capped at limit.
func (md *MessageDatabase) SearchMessages(chatId, keyword string, limit int) ([]Message, error) {
	if md.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if keyword == "" {
		return []Message{}, nil
	}
	likePattern := "%" + escapeLike(keyword) + "%"

	var rows *sql.Rows
	var err error
	if chatId != "" {
		rows, err = md.db.Query(`
			SELECT `+msgColumns+`
			FROM messages
			WHERE chat_id = ? AND text LIKE ? ESCAPE '\'
			ORDER BY timestamp DESC LIMIT ?`, chatId, likePattern, limit)
	} else {
		rows, err = md.db.Query(`
			SELECT `+msgColumns+`
			FROM messages
			WHERE text LIKE ? ESCAPE '\'
			ORDER BY timestamp DESC LIMIT ?`, likePattern, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to search messages: %w", err)
	}
	defer rows.Close()

	return collectMessages(rows)
}

// GetMessagesPaginated returns up to `limit` messages older than `beforeTimestamp` for a chat,
// ordered newest-first (so caller can reverse for display).
func (md *MessageDatabase) GetMessagesPaginated(chatId string, beforeTimestamp uint64, limit int) ([]Message, error) {
	if md.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := md.db.Query(`
		SELECT `+msgColumns+`
		FROM messages
		WHERE chat_id = ? AND timestamp < ?
		ORDER BY timestamp DESC LIMIT ?`, chatId, beforeTimestamp, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query paginated messages: %w", err)
	}
	defer rows.Close()

	return collectMessages(rows)
}

// collectMessages iterates over rows and scans each into a Message (audit CQ-2).
func collectMessages(rows *sql.Rows) ([]Message, error) {
	msgs := make([]Message, 0)
	for rows.Next() {
		var msg Message
		var ts int64
		if err := rows.Scan(&msg.Id, &msg.ChatId, &msg.ContactId, &msg.ContactName, &msg.ContactShort, &ts,
			&msg.FromMe, &msg.Forwarded, &msg.Text, &msg.MediaType, &msg.Media,
			&msg.QuotedID, &msg.QuotedSender, &msg.QuotedText, &msg.Status); err != nil {
			return msgs, fmt.Errorf("failed to scan message row: %w", err)
		}
		msg.Timestamp = uint64(ts)
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return msgs, fmt.Errorf("error iterating message rows: %w", err)
	}
	return msgs, nil
}

// nullBytes stores empty media as NULL so the upsert can tell "no media yet".
func nullBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

// GetLatestMessages returns the newest `limit` messages of a chat in
// chronological order.
func (md *MessageDatabase) GetLatestMessages(chatId string, limit int) ([]Message, error) {
	if md.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	// Reactions used to be stored as "[REACTION] …" messages; skip those.
	rows, err := md.db.Query(`SELECT * FROM (
		SELECT `+msgColumns+` FROM messages WHERE chat_id = ? AND text NOT LIKE '[REACTION]%'
		ORDER BY timestamp DESC LIMIT ?) ORDER BY timestamp ASC`, chatId, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest messages: %w", err)
	}
	defer rows.Close()
	return collectMessages(rows)
}

// GetMessage returns a single message by ID.
func (md *MessageDatabase) GetMessage(id string) (Message, error) {
	if md.db == nil {
		return Message{}, fmt.Errorf("database not initialized")
	}
	rows, err := md.db.Query(`SELECT `+msgColumns+` FROM messages WHERE id = ?`, id)
	if err != nil {
		return Message{}, fmt.Errorf("failed to query message: %w", err)
	}
	defer rows.Close()
	msgs, err := collectMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, sql.ErrNoRows
	}
	return msgs[0], nil
}

// GetOldestMessage returns the oldest stored message of a chat.
func (md *MessageDatabase) GetOldestMessage(chatId string) (Message, error) {
	if md.db == nil {
		return Message{}, fmt.Errorf("database not initialized")
	}
	rows, err := md.db.Query(`SELECT `+msgColumns+` FROM messages WHERE chat_id = ?
		ORDER BY timestamp ASC LIMIT 1`, chatId)
	if err != nil {
		return Message{}, fmt.Errorf("failed to query oldest message: %w", err)
	}
	defer rows.Close()
	msgs, err := collectMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, sql.ErrNoRows
	}
	return msgs[0], nil
}

// SetReaction stores (or, with an empty emoji, removes) a reaction.
func (md *MessageDatabase) SetReaction(msgID, sender, emoji string, ts int64) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	var err error
	if emoji == "" {
		_, err = md.db.Exec(`DELETE FROM reactions WHERE msg_id = ? AND sender = ?`, msgID, sender)
	} else {
		// Keep the newest reaction when events arrive out of order.
		_, err = md.db.Exec(`
		INSERT INTO reactions (msg_id, sender, emoji, timestamp) VALUES (?, ?, ?, ?)
		ON CONFLICT(msg_id, sender) DO UPDATE SET emoji = excluded.emoji, timestamp = excluded.timestamp
		WHERE excluded.timestamp >= reactions.timestamp`, msgID, sender, emoji, ts)
	}
	return err
}

// GetReactions returns the reactions for the given messages, oldest first.
func (md *MessageDatabase) GetReactions(msgIDs []string) (map[string][]Reaction, error) {
	out := map[string][]Reaction{}
	if md.db == nil {
		return out, fmt.Errorf("database not initialized")
	}
	for start := 0; start < len(msgIDs); start += 500 { // SQLite variable limit
		batch := msgIDs[start:min(start+500, len(msgIDs))]
		args := make([]interface{}, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		rows, err := md.db.Query(`SELECT msg_id, sender, emoji FROM reactions WHERE msg_id IN (?`+
			strings.Repeat(",?", len(batch)-1)+`) ORDER BY timestamp ASC`, args...)
		if err != nil {
			return out, fmt.Errorf("failed to query reactions: %w", err)
		}
		for rows.Next() {
			var id string
			var r Reaction
			if err := rows.Scan(&id, &r.Sender, &r.Emoji); err != nil {
				rows.Close()
				return out, err
			}
			out[id] = append(out[id], r)
		}
		rows.Close()
	}
	return out, nil
}

// MergeChat moves everything stored under chat `from` to chat `to` (the same
// person under another address): messages, their sender IDs and reactions.
// The `from` conversation row is deleted; the caller updates `to`.
func (md *MessageDatabase) MergeChat(from, to string) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	tx, err := md.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`UPDATE messages SET chat_id = ? WHERE chat_id = ?`,
		`UPDATE messages SET contact_id = ? WHERE contact_id = ?`,
		`UPDATE messages SET quoted_sender = ? WHERE quoted_sender = ?`,
		`UPDATE OR REPLACE reactions SET sender = ? WHERE sender = ?`,
	} {
		if _, err := tx.Exec(q, to, from); err != nil {
			return fmt.Errorf("merge chat: %w", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM conversations WHERE jid = ?`, from); err != nil {
		return fmt.Errorf("merge chat: %w", err)
	}
	return tx.Commit()
}

// AdvanceStatus moves a message's delivery state forward (never back) and
// reports whether it changed.
func (md *MessageDatabase) AdvanceStatus(id string, status int) (bool, error) {
	if md.db == nil {
		return false, fmt.Errorf("database not initialized")
	}
	res, err := md.db.Exec(`UPDATE messages SET status = ? WHERE id = ? AND COALESCE(status, 0) < ?`, status, id, status)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetStatus sets a message's delivery state (used by the sender: pending,
// sent, failed). A receipt that already arrived is kept.
func (md *MessageDatabase) SetStatus(id string, status int) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := md.db.Exec(`UPDATE messages SET status = ? WHERE id = ? AND COALESCE(status, 0) < 3`, status, id)
	return err
}

// SetMedia replaces a message's stored media (after an upload completes).
func (md *MessageDatabase) SetMedia(id string, media []byte) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := md.db.Exec(`UPDATE messages SET media = ? WHERE id = ?`, media, id)
	return err
}

// GetMessagesFrom returns a chat's messages from timestamp `from` onwards in
// chronological order, at most the newest `limit` of them.
func (md *MessageDatabase) GetMessagesFrom(chatId string, from uint64, limit int) ([]Message, error) {
	if md.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := md.db.Query(`SELECT * FROM (
		SELECT `+msgColumns+` FROM messages
		WHERE chat_id = ? AND timestamp >= ? AND text NOT LIKE '[REACTION]%'
		ORDER BY timestamp DESC LIMIT ?) ORDER BY timestamp ASC`, chatId, from, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()
	return collectMessages(rows)
}

// GetMessagesWindow returns up to limit messages of a chat from timestamp
// `from` onwards, oldest first.
func (md *MessageDatabase) GetMessagesWindow(chatId string, from uint64, limit int) ([]Message, error) {
	if md.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := md.db.Query(`SELECT `+msgColumns+` FROM messages
		WHERE chat_id = ? AND timestamp >= ? AND text NOT LIKE '[REACTION]%'
		ORDER BY timestamp ASC LIMIT ?`, chatId, from, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()
	return collectMessages(rows)
}

// GetRecentMedia returns the newest messages of a media type that carry
// downloadable media, from all chats, newest first.
func (md *MessageDatabase) GetRecentMedia(mediaType string, limit int) ([]Message, error) {
	if md.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := md.db.Query(`SELECT `+msgColumns+` FROM messages
		WHERE media_type = ? AND media IS NOT NULL
		ORDER BY timestamp DESC LIMIT ?`, mediaType, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query media: %w", err)
	}
	defer rows.Close()
	return collectMessages(rows)
}

// DeleteMessage removes one message (and its reactions).
func (md *MessageDatabase) DeleteMessage(id string) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if _, err := md.db.Exec(`DELETE FROM messages WHERE id = ?`, id); err != nil {
		return err
	}
	_, err := md.db.Exec(`DELETE FROM reactions WHERE msg_id = ?`, id)
	return err
}

// MarkRevoked replaces a message deleted for everyone with a note.
func (md *MessageDatabase) MarkRevoked(id, note string) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := md.db.Exec(`UPDATE messages SET text = ?, media_type = '', media = NULL,
		quoted_id = '', quoted_sender = '', quoted_text = '' WHERE id = ?`, note, id)
	if err == nil {
		_, err = md.db.Exec(`DELETE FROM reactions WHERE msg_id = ?`, id)
	}
	return err
}

// DeleteChat removes a chat and all its messages.
func (md *MessageDatabase) DeleteChat(chat string) error {
	if md.db == nil {
		return fmt.Errorf("database not initialized")
	}
	tx, err := md.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM reactions WHERE msg_id IN (SELECT id FROM messages WHERE chat_id = ?)`,
		`DELETE FROM messages WHERE chat_id = ?`,
		`DELETE FROM conversations WHERE jid = ?`,
	} {
		if _, err := tx.Exec(q, chat); err != nil {
			return fmt.Errorf("delete chat: %w", err)
		}
	}
	return tx.Commit()
}
