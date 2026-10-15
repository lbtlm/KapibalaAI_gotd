package gateway

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

var publicGroupName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)
var privateGroupHash = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Parse only Telegram group links; never fetch user-supplied URLs.
func parseGroupLink(raw string) (name, hash string, err error) {
	raw = strings.TrimSpace(raw)
	invalid := &APIError{400, "INVALID_GROUP_LINK", 0}
	if strings.HasPrefix(raw, "@") {
		name = strings.TrimPrefix(raw, "@")
	} else {
		if strings.HasPrefix(raw, "t.me/") {
			raw = "https://" + raw
		}
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host != "t.me" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", "", invalid
		}
		path := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), "/")
		switch {
		case strings.HasPrefix(path, "+"):
			hash = strings.TrimPrefix(path, "+")
		case strings.HasPrefix(path, "joinchat/"):
			hash = strings.TrimPrefix(path, "joinchat/")
		default:
			name = path
		}
	}
	if name != "" && publicGroupName.MatchString(name) {
		return name, "", nil
	}
	if hash != "" && len(hash) <= 128 && privateGroupHash.MatchString(hash) {
		return "", hash, nil
	}
	return "", "", invalid
}

// JoinLink uses the same managed client and cancellation fence as inspection.
// Membership is reconciled by Telegram on a retry; no second connection is opened.
func (a *TelegramAdapter) JoinLink(ctx context.Context, id, link string) (any, error) {
	name, hash, err := parseGroupLink(link)
	if err != nil {
		return nil, err
	}
	live, err := a.online(id)
	if err != nil {
		return nil, err
	}
	api := a.fencedClient(live.api)
	if hash != "" {
		invite, e := api.MessagesCheckChatInvite(ctx, hash)
		if e != nil {
			return nil, platformError(e)
		}
		switch v := invite.(type) {
		case *tg.ChatInviteAlready:
			return map[string]string{"status": "already_member"}, nil
		case *tg.ChatInvite:
			if v.Broadcast {
				return nil, &APIError{400, "NOT_A_GROUP", 0}
			}
		case *tg.ChatInvitePeek:
			if ch, ok := v.Chat.(*tg.Channel); ok && ch.Broadcast {
				return nil, &APIError{400, "NOT_A_GROUP", 0}
			}
		}
		_, err = api.MessagesImportChatInvite(ctx, hash)
	} else {
		resolved, e := api.ContactsResolveUsername(ctx, name)
		if e != nil {
			return nil, platformError(e)
		}
		var channel *tg.Channel
		for _, chat := range resolved.Chats {
			if ch, ok := chat.(*tg.Channel); ok && (ch.Megagroup || ch.Gigagroup) && !ch.Broadcast {
				channel = ch
				break
			}
		}
		if channel == nil {
			return nil, &APIError{400, "NOT_A_GROUP", 0}
		}
		_, err = api.ChannelsJoinChannel(ctx, &tg.InputChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash})
	}
	if tgerr.Is(err, "INVITE_REQUEST_SENT") {
		return map[string]string{"status": "approval_pending"}, nil
	}
	if tgerr.Is(err, "USER_ALREADY_PARTICIPANT") {
		return map[string]string{"status": "already_member"}, nil
	}
	if err != nil {
		return nil, platformError(err)
	}
	return map[string]string{"status": "joined"}, nil
}
