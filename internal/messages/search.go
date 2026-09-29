package messages

import (
	"context"
	"fmt"
	"strings"
)

// Search limits: how many matches are counted, and how many messages may be
// loaded to show the oldest of them.
const (
	searchMaxMatches  = 5000
	searchMaxMessages = 3000
)

// SearchHistory searches a chat's whole stored history for query. It
// returns the chat's messages from the oldest match onwards (so every match
// can be shown; capped at searchMaxMessages) and the number of matches in
// the database. Matching here is case-insensitive; the UI narrows it.
func (sm *SessionManager) SearchHistory(ctx context.Context, chat, query string) ([]Message, int, error) {
	matches, err := sm.db.SearchMessages(chat, query, searchMaxMatches)
	if err != nil {
		return nil, 0, fmt.Errorf("search: %w", err)
	}
	if len(matches) == 0 {
		return nil, 0, nil
	}
	oldest := matches[len(matches)-1].Timestamp // newest first
	msgs, err := sm.db.GetMessagesFrom(chat, oldest, searchMaxMessages)
	if err != nil {
		return nil, 0, fmt.Errorf("search: %w", err)
	}
	if len(msgs) < screenLimit { // never show fewer than a normal chat
		if latest, err := sm.db.GetLatestMessages(chat, screenLimit); err == nil {
			msgs = latest
		}
	}
	sm.resolveSenders(msgs)
	sm.attachReactions(msgs)
	sm.selfChatRead(chat, msgs)
	sm.resolveMentions(msgs)
	return msgs, len(matches), ctx.Err()
}

// SearchHit is one result of a search across all chats.
type SearchHit struct {
	Message
	ChatName string
}

// searchAllLimit caps results of a search across all chats.
const searchAllLimit = 300

// SearchAll searches message text across every chat, newest first.
func (sm *SessionManager) SearchAll(ctx context.Context, query string) ([]SearchHit, error) {
	msgs, err := sm.db.SearchMessages("", query, searchAllLimit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	sm.resolveSenders(msgs)
	sm.resolveMentions(msgs)
	hits := make([]SearchHit, 0, len(msgs))
	names := map[string]string{}
	for _, m := range msgs {
		if strings.HasPrefix(m.Text, "[REACTION]") {
			continue
		}
		name, ok := names[m.ChatId]
		if !ok {
			name = sm.ChatName(ctx, m.ChatId)
			names[m.ChatId] = name
		}
		hits = append(hits, SearchHit{Message: m, ChatName: name})
	}
	return hits, ctx.Err()
}

// LoadAround returns a chat's messages starting at msgID and running to the
// newest (at most searchMaxMessages; when there are more, the window starts
// at msgID and stops early), so a search hit can be shown in its chat.
func (sm *SessionManager) LoadAround(ctx context.Context, chat, msgID string) ([]Message, error) {
	target, err := sm.db.GetMessage(msgID)
	if err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}
	msgs, err := sm.db.GetMessagesFrom(chat, target.Timestamp, searchMaxMessages)
	if err != nil {
		return nil, err
	}
	found := false
	for _, m := range msgs {
		if m.Id == msgID {
			found = true
			break
		}
	}
	if !found { // too many newer messages: take a window from the hit onwards
		if msgs, err = sm.db.GetMessagesWindow(chat, target.Timestamp, searchMaxMessages); err != nil {
			return nil, err
		}
	}
	if len(msgs) < screenLimit {
		if latest, err := sm.db.GetLatestMessages(chat, screenLimit); err == nil && len(latest) > len(msgs) {
			msgs = latest // the hit is recent: show a normal screen
		}
	}
	sm.resolveSenders(msgs)
	sm.attachReactions(msgs)
	sm.selfChatRead(chat, msgs)
	sm.resolveMentions(msgs)
	return msgs, ctx.Err()
}
