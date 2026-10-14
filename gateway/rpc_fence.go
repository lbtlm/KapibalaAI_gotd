package gateway

import (
	"context"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

type serviceInvoker struct {
	service *Service
	remote  tg.Invoker
}

func (i serviceInvoker) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	opCtx, stop, e := i.service.operationContext(ctx)
	if e != nil {
		return e
	}
	defer stop()
	return i.remote.Invoke(opCtx, in, out)
}
func (a *TelegramAdapter) fencedClient(api *tg.Client) *tg.Client {
	return tg.NewClient(serviceInvoker{a.service, api.Invoker()})
}
