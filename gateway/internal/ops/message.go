package ops

import (
	"context"

	"github.com/gotd/td/tg"
)

// SendMessage sends a message via messages.sendMessage.
func SendMessage(ctx context.Context, api *tg.Client, req *tg.MessagesSendMessageRequest) (tg.UpdatesClass, error) {
	return api.MessagesSendMessage(ctx, req)
}
