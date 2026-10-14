package gateway_test

import (
	"context"
	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestPostgresRestartRetainsAcceptedSendAndHistory(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required for real PostgreSQL restart acceptance")
	}
	store, err := gateway.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gateway.New(context.Background(), gateway.Config{Mode: "fake", AdminEnabled: true, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	call(t, ts, "POST", "/admin/reset", nil, 200)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	gid := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "send", "code": "NETWORK_TIMEOUT", "effect": true, "settleAfterMs": 600}, 200)
	call(t, ts, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "durable-send", "text": "restart me"}, 504)
	ts.Close()
	s.Close()
	store, err = gateway.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	s, err = gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	ts = httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	time.Sleep(700 * time.Millisecond)
	m := call(t, ts, "GET", "/groups/"+gid+"/messages/by-client-id/durable-send", nil, 200)
	if m["msgId"] == "" || m["sentAt"] == nil {
		t.Fatalf("lost accepted effect: %v", m)
	}
}
