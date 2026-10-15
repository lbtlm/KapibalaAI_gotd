package platform_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/migration"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestWorkspaceUpgradeDisconnectsLegacyLiveAccountsWithoutLosingData(t *testing.T) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	var mu sync.Mutex
	var calls []string
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/accounts/") {
			mu.Lock()
			calls = append(calls, r.URL.Path)
			mu.Unlock()
		}
		base.ServeHTTP(w, r)
	})
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	st, b, _ := call(t, s, "POST", "/api/accounts/account-3/transition", token, `{"expectedFrom":"online","to":"suspended"}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	a.Close()
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	down, e := os.ReadFile(filepath.Join("..", "migration", "sql", "006_account_workspace.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	// A version-five snapshot has no incarnation field; preserve its real groups
	// and persisted queued-message identity while restoring its live account states.
	_, e = pool.Exec(context.Background(), string(down)+`; UPDATE schema_migrations SET version=5,dirty=false; UPDATE accounts SET status='online' WHERE id='account-1'; UPDATE accounts SET status='rate_limited',rate_limited_until=now()+interval '60 seconds' WHERE id='account-2'`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(context.Background(), `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES($1,'legacy-queued','account-2','p-account-2',true,'legacy synthetic message','queued')`, group)
	if e != nil {
		t.Fatal(e)
	}
	if e = migration.Apply(cfg.DatabaseURL); e != nil {
		t.Fatal(e)
	}
	reopened, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	restored := httptest.NewServer(reopened.Handler())
	defer restored.Close()
	for id, want := range map[string]string{"account-1": "disconnected", "account-2": "disconnected", "account-3": "suspended"} {
		st, b, _ = call(t, restored, "GET", "/api/accounts/"+id, token, "")
		if st != 200 || b["status"] != want || b["rateLimitedUntil"] != nil {
			t.Fatalf("legacy state failed to converge: %s %d %v", id, st, b)
		}
	}
	st, b, _ = call(t, restored, "GET", "/api/groups/"+group, token, "")
	if st != 200 || len(b["members"].([]any)) != 2 {
		t.Fatalf("upgrade changed members: %d %v", st, b)
	}
	st, b, _ = call(t, restored, "GET", "/api/groups/"+group+"/messages", token, "")
	items := b["items"].([]any)
	if st != 200 || len(items) != 1 || items[0].(map[string]any)["clientMsgId"] != "legacy-queued" || items[0].(map[string]any)["deliveryStatus"] != "queued" {
		t.Fatalf("upgrade lost durable send identity: %d %v", st, b)
	}
	for _, id := range []string{"account-1", "account-2"} {
		st, b, _ = call(t, restored, "GET", "/api/accounts/"+id+"/logs?action=upgrade_disconnect", token, "")
		if st != 200 || len(b["items"].([]any)) != 1 {
			t.Fatalf("missing upgrade log: %s %d %v", id, st, b)
		}
	}
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(restored.URL, "http")+"/ws", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	c.WriteJSON(map[string]any{"type": "auth", "accessToken": token, "sinceSeq": 0})
	found := map[string]bool{}
	for len(found) < 2 {
		var frame map[string]any
		if e = c.ReadJSON(&frame); e != nil {
			t.Fatal(e)
		}
		if frame["type"] == "account_status_changed" {
			p := frame["payload"].(map[string]any)
			if p["to"] == "disconnected" {
				found[p["accountId"].(string)] = true
			}
		}
	}
	reopened.Close()
	cmd, live := childServer(t, cfg)
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	eventually(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		count := 0
		for _, p := range calls {
			if strings.HasSuffix(p, "/disconnect") {
				count++
			}
		}
		return count == 2
	})
	mu.Lock()
	before := len(calls)
	mu.Unlock()
	st, b, _ = call(t, live, "POST", "/api/accounts/account-1/connect", token, `{}`)
	if st != 200 {
		t.Fatalf("explicit connect after cleanup: %d %v", st, b)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != before+1 || calls[len(calls)-1] != "/accounts/account-1/connect" {
		t.Fatalf("unexpected automatic connection during upgrade: %v", calls)
	}
}

func TestInspectionRateLimitsBlockQueuePreserveSnapshotAndRetryDetails(t *testing.T) {
	for _, operation := range []string{"check", "sync"} {
		t.Run(operation, func(t *testing.T) {
			g := gatewayFixture(t)
			base := g.server.Config.Handler
			var limited atomic.Bool
			var sends atomic.Int32
			var sendTime atomic.Int64
			var retryMS atomic.Int64
			retryMS.Store(1200)
			g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/send") {
					sendTime.Store(time.Now().UnixNano())
					sends.Add(1)
				}
				if r.URL.Path == "/accounts/account-2/check" || (r.URL.Path == "/accounts/account-2/joined-groups") {
					if limited.Load() {
						w.WriteHeader(429)
						fmt.Fprintf(w, `{"code":"RATE_LIMITED","retryAfterSeconds":%.3f}`, float64(retryMS.Load())/1000)
						return
					}
					fmt.Fprint(w, `{"items":[{"id":"existing","title":"Cached group","username":null,"type":"group","role":"member"}]}`)
					return
				}
				base.ServeHTTP(w, r)
			})
			a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
			a.Start(context.Background())
			token, _ := login(t, s, "admin")
			group := createGroup(t, s, token)
			st, b, _ := call(t, s, "POST", "/api/accounts/account-2/joined-groups/sync", token, `{}`)
			if st != 200 {
				t.Fatal(st, b)
			}
			synced := b["lastSyncedAt"]
			limited.Store(true)
			path := "/api/accounts/account-2/check"
			body := `{"connectIfNeeded":false}`
			if operation == "sync" {
				path = "/api/accounts/account-2/joined-groups/sync"
				body = `{}`
			}
			start := time.Now()
			st, b, _ = call(t, s, "POST", path, token, body)
			if st != 429 || b["error"].(map[string]any)["retryAfterSeconds"] != float64(1.2) {
				t.Fatalf("inspection discarded retry details: %d %v", st, b)
			}
			st, b, _ = call(t, s, "GET", "/api/accounts/account-2", token, "")
			if st != 200 || b["status"] != "rate_limited" || b["rateLimitedUntil"] == nil {
				t.Fatalf("inspection did not back off account: %d %v", st, b)
			}
			until, e := time.Parse(time.RFC3339Nano, b["rateLimitedUntil"].(string))
			if e != nil || until.Before(start.Add(time.Second)) {
				t.Fatalf("invalid account deadline: %v %v", until, e)
			}
			if operation == "check" && b["lastCheckResult"] != "failed" {
				t.Fatal("failed check result lost")
			}
			if operation == "check" {
				retryMS.Store(100)
				st, b, _ = call(t, s, "POST", path, token, body)
				if st != 429 || b["error"].(map[string]any)["retryAfterSeconds"] != float64(0.1) {
					t.Fatal(st, b)
				}
				st, b, _ = call(t, s, "GET", "/api/accounts/account-2", token, "")
				if st != 200 || b["rateLimitedUntil"] != until.UTC().Format(time.RFC3339Nano) {
					t.Fatalf("shorter inspection retry shortened deadline: %d %v", st, b)
				}
			}
			st, b, _ = call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-2","text":"inspection backoff synthetic"}`)
			if st != 202 {
				t.Fatal(st, b)
			}
			time.Sleep(250 * time.Millisecond)
			if sends.Load() != 0 {
				t.Fatal("queued send escaped inspection backoff")
			}
			st, b, _ = call(t, s, "GET", "/api/accounts/account-2/joined-groups", token, "")
			if st != 200 || b["lastSyncedAt"] != synced || len(b["items"].([]any)) != 1 {
				t.Fatalf("rate limit replaced last snapshot: %d %v", st, b)
			}
			eventually(t, 3*time.Second, func() bool { return sends.Load() == 1 })
			if time.Unix(0, sendTime.Load()).Before(until) {
				t.Fatal("send resumed before inspection deadline")
			}
		})
	}
}

func TestInspectionDeadlineDoesNotReviveManualOfflineOrTerminal(t *testing.T) {
	for _, target := range []string{"disconnected", "suspended"} {
		t.Run(target, func(t *testing.T) {
			g := gatewayFixture(t)
			base := g.server.Config.Handler
			var sends atomic.Int32
			g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/send") {
					sends.Add(1)
				}
				if r.URL.Path == "/accounts/account-2/check" {
					w.WriteHeader(429)
					fmt.Fprint(w, `{"code":"RATE_LIMITED","retryAfterSeconds":0.5}`)
					return
				}
				base.ServeHTTP(w, r)
			})
			a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
			a.Start(context.Background())
			token, _ := login(t, s, "admin")
			group := createGroup(t, s, token)
			st, b, _ := call(t, s, "POST", "/api/accounts/account-2/check", token, `{"connectIfNeeded":false}`)
			if st != 429 {
				t.Fatal(st, b)
			}
			st, b, _ = call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-2","text":"cancel backoff synthetic"}`)
			if st != 202 {
				t.Fatal(st, b)
			}
			st, b, _ = call(t, s, "POST", "/api/accounts/account-2/transition", token, `{"expectedFrom":"rate_limited","to":"`+target+`"}`)
			if st != 200 {
				t.Fatal(st, b)
			}
			time.Sleep(700 * time.Millisecond)
			if accountStatus(t, s, token, "account-2") != target || sends.Load() != 0 {
				t.Fatalf("inspection expiry revived %s or sent queued content", target)
			}
		})
	}
}
