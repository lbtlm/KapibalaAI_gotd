package gateway

import (
	"context"
	"sort"

	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

// Inspect reuses the managed online session; it never connects or registers a group.
func (a *TelegramAdapter) Inspect(ctx context.Context, id, operation string) (any, error) {
	live, err := a.online(id)
	if err != nil {
		return nil, err
	}
	api := a.fencedClient(live.api)
	if operation == "check" {
		users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
		if err != nil {
			return nil, platformError(err)
		}
		if len(users) != 1 {
			return nil, &APIError{503, "GATEWAY_UNAVAILABLE", 0}
		}
		self, ok := users[0].(*tg.User)
		if !ok || self.ID <= 0 || self.ID != live.self.ID {
			return nil, &APIError{503, "GATEWAY_UNAVAILABLE", 0}
		}
		return AccountProfile{Source: "telegram", PlatformUserID: stringID(self.ID), Phone: optionalString(self.Phone), Username: optionalString(self.Username), FirstName: optionalString(self.FirstName), LastName: optionalString(self.LastName), Premium: self.Premium}, nil
	}
	items := []JoinedGroup{}
	seen := map[string]bool{}
	// Pinned gotd's iterator handles offset message/date/peer and page errors.
	// In particular a short messages.dialogsSlice is not the final page.
	iter := dialogs.NewQueryBuilder(api).GetDialogs().BatchSize(100).Iter()
	for iter.Next(ctx) {
		entry := iter.Value()
		var item JoinedGroup
		switch peer := entry.Dialog.GetPeer().(type) {
		case *tg.PeerChat:
			chat, ok := entry.Entities.Chat(peer.ChatID)
			if !ok || chat.Left || chat.Deactivated {
				continue
			}
			item = JoinedGroup{ID: "chat-" + stringID(chat.ID), Title: chat.Title, Type: "group", Role: "member"}
			if chat.Creator {
				item.Role = "creator"
			} else if _, ok := chat.GetAdminRights(); ok {
				item.Role = "admin"
			}
		case *tg.PeerChannel:
			channel, ok := entry.Entities.Channel(peer.ChannelID)
			if !ok || channel.Broadcast || !channel.Megagroup && !channel.Gigagroup || channel.Left {
				continue
			}
			item = JoinedGroup{ID: "tg-" + stringID(channel.ID), Title: channel.Title, Username: optionalString(channel.Username), Type: "supergroup", Role: "member"}
			if channel.Min {
				item.Role = "unknown"
			}
			if channel.Creator {
				item.Role = "creator"
			} else if _, ok := channel.GetAdminRights(); ok {
				item.Role = "admin"
			}
		default:
			continue
		}
		if !seen[item.ID] {
			seen[item.ID] = true
			items = append(items, item)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, platformError(err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return map[string]any{"items": items}, nil
}
