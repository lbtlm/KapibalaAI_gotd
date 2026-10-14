package ops

import (
	"context"

	"github.com/gotd/td/tg"
)

// ExportChatInvite exports an invite link for a chat/channel.
func ExportChatInvite(ctx context.Context, api *tg.Client, req *tg.MessagesExportChatInviteRequest) (tg.ExportedChatInviteClass, error) {
	return api.MessagesExportChatInvite(ctx, req)
}

// EditChatAdmin promotes or demotes a user in a channel.
func EditChatAdmin(ctx context.Context, api *tg.Client, req *tg.ChannelsEditAdminRequest) (tg.UpdatesClass, error) {
	return api.ChannelsEditAdmin(ctx, req)
}
