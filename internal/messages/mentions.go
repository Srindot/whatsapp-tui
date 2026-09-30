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
	for _, p := range info.Participants {
		who := p.JID
		if !p.PhoneNumber.IsEmpty() {
			who = p.PhoneNumber
		}
		if who.User == own.User || p.JID.User == own.User || (!client.Store.LID.IsEmpty() && p.JID.User == client.Store.LID.User) {
			continue
		}
		name := strings.TrimPrefix(sm.contactName(ctx, who), "~ ")
		out = append(out, GroupMember{JID: p.JID.ToNonAD().String(), Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// SendText sends text that @mentions the given JIDs (their numbers must
// appear in the text as "@<number>"). Safe from any goroutine.
func (sm *SessionManager) SendText(ctx context.Context, chat, text string, mentions []string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
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
	return false
}
