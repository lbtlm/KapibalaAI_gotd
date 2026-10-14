package ops

import (
	"context"

	"github.com/gotd/td/tg"
)

// JoinChannel joins a channel or supergroup.
func JoinChannel(ctx context.Context, api *tg.Client, input *tg.InputChannel) (tg.UpdatesClass, error) {
	return api.ChannelsJoinChannel(ctx, input)
}

// LeaveChannel leaves a channel or supergroup.
func LeaveChannel(ctx context.Context, api *tg.Client, input *tg.InputChannel) (tg.UpdatesClass, error) {
	return api.ChannelsLeaveChannel(ctx, input)
}

// ImportChatInvite joins a private chat via invite hash.
func ImportChatInvite(ctx context.Context, api *tg.Client, hash string) (tg.UpdatesClass, error) {
	return api.MessagesImportChatInvite(ctx, hash)
}

// CreateChannel creates a channel or megagroup.
func CreateChannel(ctx context.Context, api *tg.Client, req *tg.ChannelsCreateChannelRequest) (tg.UpdatesClass, error) {
	return api.ChannelsCreateChannel(ctx, req)
}

// GetParticipants fetches channel participants.
func GetParticipants(ctx context.Context, api *tg.Client, req *tg.ChannelsGetParticipantsRequest) (tg.ChannelsChannelParticipantsClass, error) {
	return api.ChannelsGetParticipants(ctx, req)
}
