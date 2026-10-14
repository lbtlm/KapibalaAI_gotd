package gateway_test

import (
	"context"
	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
	"testing"
)

func TestGatewayRefusesMixedFakeAndTelegramDatabase(t *testing.T) {
	store := gateway.NewMemoryStore()
	fake, e := gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store})
	if e != nil {
		t.Fatal(e)
	}
	fake.Close()
	adapter, e := gateway.NewTelegramAdapter(context.Background(), nil, []gateway.AccountConfig{{ID: "account-1", APIID: 1, APIHash: "synthetic", SessionPath: "unread-synthetic", SessionFormat: "gotdJSON"}})
	if e != nil {
		t.Fatal(e)
	}
	defer adapter.Close()
	real, e := gateway.New(context.Background(), gateway.Config{Mode: "telegram", Store: store, Telegram: adapter})
	if e == nil {
		real.Close()
		t.Fatal("fake persisted state silently accepted in telegram mode")
	}
}
