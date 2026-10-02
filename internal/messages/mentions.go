package messages

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// mentionRe finds "@<number>" mentions as WhatsApp writes them in text:
// the phone number, or in LID-addressed groups the hidden LID number.
var mentionRe = regexp.MustCompile(`@(\d{5,})`)

// mentionName names the user behind a mention number, or "" if unknown.
func (sm *SessionManager) mentionName(ctx context.Context, user string) string {
	if own, err := sm.ownJID(); err == nil && own.User == user {
		return "You"
	}
	if client := sm.getClient(); client != nil && client.Store.LID.User == user && user != "" {
		return "You"
	}
	for _, server := range []string{types.DefaultUserServer, types.HiddenUserServer} {
		jid := types.NewJID(user, server)
		name := sm.contactName(ctx, jid)
		if !isPlaceholderName(name, user) {
			return strings.TrimPrefix(name, "~ ")
		}
		if server == types.HiddenUserServer {
			if pn := sm.pnForLID(ctx, jid); pn.Server == types.DefaultUserServer {
				return formatPhone(pn.User) // at least the real number
			}
		}
	}
	return ""
}

// resolveMentions fills Message.Mentions with names for "@<number>"s.
func (sm *SessionManager) resolveMentions(msgs []Message) {
	markMentionAll(msgs)
	if sm.getClient() == nil {
		return
	}
	ctx := context.Background()
	cache := map[string]string{}
	for i, m := range msgs {
		for _, match := range mentionRe.FindAllStringSubmatch(m.Text, -1) {
			user := match[1]
			name, ok := cache[user]
			if !ok {
				name = sm.mentionName(ctx, user)
				cache[user] = name
			}
			if name == "" {
				continue
			}
			if msgs[i].Mentions == nil {
				msgs[i].Mentions = map[string]string{}
			}
			msgs[i].Mentions[user] = name
		}
	}
}

// GroupMember is someone you can @mention in a group.
type GroupMember struct {
	JID  string // the address to mention (LID or phone number, as the group uses)
	Name string
}

// MentionAll is the member-list entry (JID and name) for "@all": everyone
// in the group gets a mention.
const MentionAll = "all"

// mentionAllMax is the largest group where anyone may @all; in bigger ones
// only admins can (WhatsApp's own rule).
const mentionAllMax = 32

// mentionAllRe finds "@all" as a word.
var mentionAllRe = regexp.MustCompile(`(?:^|\s)@all\b`)

// markMentionAll marks "@all" in someone else's group message as a
// mention of you.
func markMentionAll(msgs []Message) {
	for i, m := range msgs {
		if m.FromMe || !strings.HasSuffix(m.ChatId, "@"+types.GroupServer) || !HasMentionAll(m.Text) {
			continue
		}
		if msgs[i].Mentions == nil {
			msgs[i].Mentions = map[string]string{}
		}
		msgs[i].Mentions[MentionAll] = "You"
	}
}

// HasMentionAll reports whether text mentions "@all".
func HasMentionAll(text string) bool { return mentionAllRe.MatchString(text) }

// GroupMembers lists a group's members (without you) for the mention
// picker, sorted by name.
func (sm *SessionManager) GroupMembers(ctx context.Context, chat string) ([]GroupMember, error) {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return nil, errors.New("not connected to WhatsApp")
	}
	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server != types.GroupServer {
		return nil, errors.New("not a group")
	}
	info, err := client.GetGroupInfo(ctx, jid)
	if err != nil {
		return nil, fmt.Errorf("group members: %w", err)
	}
	own, _ := sm.ownJID()
	var out []GroupMember
	admin := false
	for _, p := range info.Participants {
		who := p.JID
		if !p.PhoneNumber.IsEmpty() {
			who = p.PhoneNumber
		}
		if who.User == own.User || p.JID.User == own.User || (!client.Store.LID.IsEmpty() && p.JID.User == client.Store.LID.User) {
			admin = p.IsAdmin || p.IsSuperAdmin
			continue
		}
		name := strings.TrimPrefix(sm.contactName(ctx, who), "~ ")
		out = append(out, GroupMember{JID: p.JID.ToNonAD().String(), Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	if len(info.Participants) <= mentionAllMax || admin {
		out = append([]GroupMember{{JID: MentionAll, Name: MentionAll}}, out...)
	}
	return out, nil
}

// expandMentions turns an "@all" in text (picked or typed) into a mention
// of every member, since that's what notifies them all; other mentions are
// kept.
func (sm *SessionManager) expandMentions(ctx context.Context, chat, text string, mentions []string) []string {
	out := make([]string, 0, len(mentions))
	all := false
	for _, j := range mentions {
		if j == MentionAll {
			all = true
			continue
		}
		out = append(out, j)
	}
	if !all && !(strings.HasSuffix(chat, "@"+types.GroupServer) && HasMentionAll(text)) {
		return out
	}
	members, err := sm.GroupMembers(ctx, chat)
	if err != nil {
		sm.debugf("@all in %s: %v", chat, err)
		return out
	}
	if len(members) == 0 || members[0].JID != MentionAll {
		return out // not allowed here (a big group, and you aren't an admin)
	}
	seen := map[string]bool{}
	for _, j := range out {
		seen[j] = true
	}
	for _, mem := range members[1:] {
		if !seen[mem.JID] {
			seen[mem.JID] = true
			out = append(out, mem.JID)
		}
	}
	return out
}

// SendText sends text that @mentions the given JIDs (their numbers must
// appear in the text as "@<number>"). Safe from any goroutine.
func (sm *SessionManager) SendText(ctx context.Context, chat, text string, mentions []string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
	mentions = sm.expandMentions(ctx, chat, text, mentions)
	msg := &waE2E.Message{Conversation: proto.String(text)}
	if len(mentions) > 0 {
		msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(text),
			ContextInfo: &waE2E.ContextInfo{MentionedJID: mentions},
		}}
	}
	_, err = sm.sendTracked(ctx, jid, msg, Message{Text: text}, truncatePreview(text))
	return err
}

// mentionsMe reports whether a message @mentions you (by phone number or
// by your hidden LID).
func (sm *SessionManager) mentionsMe(msg *waE2E.Message) bool {
	client := sm.getClient()
	if client == nil || client.Store.ID == nil {
		return false
	}
	for _, j := range contextInfo(msg).GetMentionedJID() {
		jid, err := types.ParseJID(j)
		if err != nil {
			continue
		}
		if jid.User == client.Store.ID.User || (!client.Store.LID.IsEmpty() && jid.User == client.Store.LID.User) {
			return true
		}
	}
	// WhatsApp's own "@all" doesn't list members: it's the text plus a
	// non-JID mention
	if contextInfo(msg).GetNonJIDMentions() > 0 {
		text, _ := extractMessageContent(msg)
		return HasMentionAll(text)
	}
	return false
}
