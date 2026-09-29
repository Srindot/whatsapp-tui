package messages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

var errNoProfilePicture = errors.New("no profile picture")

// ChatInfo is what the info panel shows for a chat.
type ChatInfo struct {
	JID     string
	Name    string
	IsGroup bool

	Phone string // contacts: formatted number
	About string // contacts: status text; groups: description

	Created      time.Time // groups
	Participants []string  // groups: display names, admins first
	Admins       int

	Picture string // local path of the full-size picture, "" if none
}

// ChatInfo fetches details about a chat. It blocks on the network.
func (sm *SessionManager) ChatInfo(ctx context.Context, jidStr string) (ChatInfo, error) {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return ChatInfo{}, errors.New("not connected to WhatsApp")
	}
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return ChatInfo{}, fmt.Errorf("invalid JID: %w", err)
	}
	info := ChatInfo{JID: jidStr, IsGroup: jid.Server == types.GroupServer}

	if info.IsGroup {
		g, err := client.GetGroupInfo(ctx, jid)
		if err != nil {
			return info, fmt.Errorf("group info: %w", err)
		}
		info.Name, info.About, info.Created = g.Name, g.Topic, g.GroupCreated
		type member struct {
			name  string
			admin bool
		}
		var ms []member
		for _, p := range g.Participants {
			who := p.JID
			if !p.PhoneNumber.IsEmpty() {
				who = p.PhoneNumber
			}
			name := sm.contactName(ctx, who)
			if own, err := sm.ownJID(); err == nil && (who.User == own.User || p.JID.User == own.User) {
				name = "You"
			}
			admin := p.IsAdmin || p.IsSuperAdmin
			if admin {
				info.Admins++
			}
			ms = append(ms, member{name, admin})
		}
		sort.SliceStable(ms, func(i, j int) bool {
			if ms[i].admin != ms[j].admin {
				return ms[i].admin
			}
			return strings.ToLower(strings.TrimPrefix(ms[i].name, "~ ")) < strings.ToLower(strings.TrimPrefix(ms[j].name, "~ "))
		})
		for _, m := range ms {
			if m.admin {
				m.name += " (admin)"
			}
			info.Participants = append(info.Participants, m.name)
		}
	} else {
		info.Name = sm.contactName(ctx, jid)
		info.Phone = formatPhone(jid.User)
		if users, err := client.GetUserInfo(ctx, []types.JID{jid}); err == nil {
			info.About = users[jid].Status
		}
	}

	if path, err := sm.fullPicture(ctx, client, jid); err == nil {
		info.Picture = path
	} else {
		sm.debugf("full picture for %s: %v", jidStr, err)
	}
	return info, nil
}

// fullPicture downloads the full-size profile picture into the cache.
func (sm *SessionManager) fullPicture(ctx context.Context, client *whatsmeow.Client, jid types.JID) (string, error) {
	dir, err := cacheDir("avatars")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeName(jid.String())+".full.jpg")
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < avatarTTL {
		return path, nil
	}
	pic, err := client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{})
	if err != nil {
		return "", err
	}
	if pic == nil || pic.URL == "" {
		return "", errNoProfilePicture
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pic.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// mediaFileName picks a file name for saving m's media, like the phone's
// "IMG-20260929-WA0001.jpg" (with the message ID instead of a counter), or a
// document's own name.
func mediaFileName(m Message) string {
	var pm waE2E.Message
	_ = proto.Unmarshal(m.Media, &pm)
	ts := time.Unix(int64(m.Timestamp), 0).Format("20060102")
	id := safeName(m.Id)
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	ext := func(mimetype, fallback string) string {
		if mimetype = strings.TrimSpace(strings.Split(mimetype, ";")[0]); mimetype != "" {
			// mime.ExtensionsByType sorts alphabetically (.f4v before .mp4),
			// so pin the common types.
			if e, ok := commonExt[mimetype]; ok {
				return e
			}
			if exts, _ := mime.ExtensionsByType(mimetype); len(exts) > 0 {
				return exts[0]
			}
		}
		return fallback
	}
	switch {
	case pm.GetDocumentMessage() != nil:
		d := pm.GetDocumentMessage()
		if name := filepath.Base(d.GetFileName()); name != "" && name != "." && name != "/" {
			return name
		}
		return "DOC-" + ts + "-" + id + ext(d.GetMimetype(), "")
	case pm.GetImageMessage() != nil:
		return "IMG-" + ts + "-" + id + ext(pm.GetImageMessage().GetMimetype(), ".jpg")
	case pm.GetStickerMessage() != nil:
		return "STK-" + ts + "-" + id + ".webp"
	case pm.GetVideoMessage() != nil:
		return "VID-" + ts + "-" + id + ext(pm.GetVideoMessage().GetMimetype(), ".mp4")
	case pm.GetAudioMessage() != nil:
		return "AUD-" + ts + "-" + id + ext(pm.GetAudioMessage().GetMimetype(), ".ogg")
	}
	return "FILE-" + ts + "-" + id
}

var commonExt = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
	"video/mp4": ".mp4", "video/3gpp": ".3gp", "video/quicktime": ".mov",
	"audio/ogg": ".ogg", "audio/mpeg": ".mp3", "audio/mp4": ".m4a", "audio/aac": ".aac",
	"application/pdf": ".pdf",
}

// SaveMedia downloads a message's media into dir and returns the saved path.
// Existing files are not overwritten; a number is added instead.
func (sm *SessionManager) SaveMedia(ctx context.Context, msgID, dir string) (string, error) {
	msg, err := sm.db.GetMessage(msgID)
	if err != nil {
		return "", fmt.Errorf("load message: %w", err)
	}
	if len(msg.Media) == 0 {
		return "", errors.New("this message has no downloadable media (it arrived before media support)")
	}
	src, err := sm.DownloadMedia(ctx, msgID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	name := mediaFileName(msg)
	base, ext := strings.TrimSuffix(name, filepath.Ext(name)), filepath.Ext(name)
	for i := 0; ; i++ {
		path := filepath.Join(dir, name)
		if i > 0 {
			path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return "", err
		}
		return path, f.Close()
	}
}

// MediaPath returns the local file of a message's media, downloading it if
// needed (for copying or opening it).
func (sm *SessionManager) MediaPath(ctx context.Context, msgID string) (string, error) {
	return sm.DownloadMedia(ctx, msgID)
}

// FullPicture returns the local path of a chat's full-size profile picture
// (downloading it if needed), or "" when there is none.
func (sm *SessionManager) FullPicture(ctx context.Context, jidStr string) (string, error) {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return "", errors.New("not connected to WhatsApp")
	}
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return "", fmt.Errorf("invalid JID: %w", err)
	}
	path, err := sm.fullPicture(ctx, client, jid)
	if errors.Is(err, whatsmeow.ErrProfilePictureNotSet) || errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) ||
		errors.Is(err, errNoProfilePicture) {
		return "", nil
	}
	return path, err
}
