package gateway

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gotd/td/tg"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type updateFailStore struct {
	*PostgresStore
	fail atomic.Bool
}

func (s *updateFailStore) Save(ctx context.Context, b []byte) error {
	if s.fail.CompareAndSwap(true, false) {
		return errors.New("injected event-commit failure")
	}
	return s.PostgresStore.Save(ctx, b)
}
func updateTestService(t *testing.T, store Store, db *sql.DB, remote *remoteTelegram) (*Service, *testRPCConnection, *httptest.Server) {
	t.Helper()
	adapter := newTestRPCConnection(t, remote, db)
	s, e := New(context.Background(), Config{Mode: "fake", Store: store, AdminEnabled: true, Telegram: adapter})
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(s)
	status, _ := rpcCall(t, ts, "POST", "/admin/reset", nil)
	if status != 200 {
		t.Fatal(status)
	}
	status, _ = rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil)
	if status != 200 {
		t.Fatal(status)
	}
	status, _ = rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"})
	if status != 200 {
		t.Fatal(status)
	}
	return s, adapter, ts
}
func testPG(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	store, e := OpenPostgres(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	return store
}
func assertMessageReplay(t *testing.T, ts *httptest.Server, marker string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events?since=0", nil)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	scan := bufio.NewScanner(res.Body)
	for scan.Scan() {
		if line := scan.Text(); strings.HasPrefix(line, "data: ") {
			var value map[string]any
			_ = json.Unmarshal([]byte(line[6:]), &value)
			if value["type"] == "message" && value["text"] == marker {
				return
			}
		}
	}
	t.Fatal("durable message not replayable")
}
func TestDifferenceCannotCheckpointBeforeDurableEvents(t *testing.T) {
	store := testPG(t)
	fault := &updateFailStore{PostgresStore: store}
	marker := "checkpoint-replay-event"
	remote := &remoteTelegram{messages: map[int64]int{}, difference: &tg.UpdatesChannelDifference{Final: true, Pts: 88, NewMessages: []tg.MessageClass{&tg.Message{ID: 32, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Message: marker, Date: 1700000000}}}}
	s, adapter, ts := updateTestService(t, fault, store.DB, remote)
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	if e := pipeline.Storage.SetChannelPts(context.Background(), 10001, 321, 7); e != nil {
		t.Fatal(e)
	}
	fault.fail.Store(true)
	_, e := pipeline.API.UpdatesGetChannelDifference(context.Background(), &tg.UpdatesGetChannelDifferenceRequest{Channel: &tg.InputChannel{ChannelID: 321, AccessHash: 64}, Filter: &tg.ChannelMessagesFilterEmpty{}, Pts: 7, Limit: 100})
	if e == nil {
		ts.Close()
		s.Close()
		t.Fatal("difference returned before durable event acknowledgement")
	}
	if e = pipeline.Storage.SetChannelPts(context.Background(), 10001, 321, 88); e == nil {
		ts.Close()
		s.Close()
		t.Fatal("checkpoint passed failed event persistence")
	}
	ts.Close()
	s.Close()
	store = testPG(t)
	adapter = newTestRPCConnection(t, remote, store.DB)
	s, e = New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
	if e != nil {
		t.Fatal(e)
	}
	ts = httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	pipeline = adapter.Updates("account-1", tg.NewClient(remote), func() {})
	pts, found, e := pipeline.Storage.GetChannelPts(context.Background(), 10001, 321)
	if e != nil || !found || pts != 7 {
		t.Fatalf("lost restart cursor: %d %v %v", pts, found, e)
	}
	_, e = pipeline.API.UpdatesGetChannelDifference(context.Background(), &tg.UpdatesGetChannelDifferenceRequest{Channel: &tg.InputChannel{ChannelID: 321, AccessHash: 64}, Filter: &tg.ChannelMessagesFilterEmpty{}, Pts: pts, Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = pipeline.Storage.SetChannelPts(context.Background(), 10001, 321, 88); e != nil {
		t.Fatal(e)
	}
	assertMessageReplay(t, ts, marker)
}
func TestCheckpointCannotWriteAfterLeaseHandover(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	injector, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer injector.Close()
	var killed bool
	e = injector.QueryRow(`SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=761231912 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&killed)
	if e != nil || !killed {
		t.Fatal("lease injection", e)
	}
	if e = pipeline.Storage.SetChannelPts(context.Background(), 10001, 321, 999); e == nil {
		t.Fatal("stale checkpoint wrote through surviving pool")
	}
}
