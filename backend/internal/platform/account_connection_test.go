package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestConnectIntentSurvivesHardKillBeforeGatewayResponse(t *testing.T) {
	entered := make(chan string, 1)
	var mu sync.Mutex
	var ids []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/connect") {
			var b struct {
				ConnectionID string `json:"connectionId"`
			}
			json.NewDecoder(r.Body).Decode(&b)
			mu.Lock()
			ids = append(ids, b.ConnectionID)
			n := len(ids)
			mu.Unlock()
			if n == 1 {
				entered <- b.ConnectionID
				<-r.Context().Done()
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"platformUserId": "1001", "connectionId": b.ConnectionID})
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer gateway.Close()
	a, _, cfg := fixtureConfig(t, platform.Config{GatewayURL: gateway.URL})
	a.Close()
	cmd, s := childServer(t, cfg)
	token, _ := login(t, s, "admin")
	done := make(chan struct{})
	go func() {
		defer close(done)
		req, _ := http.NewRequest("POST", s.URL+"/api/accounts/account-1/connect", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		res, e := http.DefaultClient.Do(req)
		if e == nil {
			res.Body.Close()
		}
	}()
	var id string
	select {
	case id = <-entered:
	case <-time.After(4 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("gateway effect not reached")
	}
	cmd.Process.Kill()
	cmd.Wait()
	<-done
	if id == "" {
		t.Fatal("connection intent was not assigned before external effect")
	}
	restarted, restored := childServer(t, cfg)
	defer func() { restarted.Process.Kill(); restarted.Wait() }()
	st, b, _ := call(t, restored, "POST", "/api/accounts/account-1/connect", token, `{}`)
	if st != 200 {
		t.Fatalf("reconcile after hard kill: %d %v", st, b)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 || ids[1] != id {
		t.Fatalf("ambiguous connection used new incarnation: %v", ids)
	}
}

func TestConnectCommitFailureReplaysSameIncarnationAndEndedNeedsNewIntent(t *testing.T) {
	var mu sync.Mutex
	ids := []string{}
	ended := false
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/connect") {
			fmt.Fprint(w, `{}`)
			return
		}
		var b struct {
			ConnectionID string `json:"connectionId"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		ids = append(ids, b.ConnectionID)
		isEnded := ended
		mu.Unlock()
		if isEnded {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"code":"CONNECTION_ENDED"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"platformUserId": "1001", "connectionId": b.ConnectionID})
	}))
	defer g.Close()
	_, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.URL})
	token, _ := login(t, s, "admin")
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	_, e = pool.Exec(context.Background(), `CREATE FUNCTION reject_online_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='online' THEN RAISE EXCEPTION 'checkpoint unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_online_checkpoint BEFORE UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION reject_online_checkpoint()`)
	if e != nil {
		t.Fatal(e)
	}
	st, b, _ := call(t, s, "POST", "/api/accounts/account-1/connect", token, `{}`)
	if st != 500 {
		t.Fatalf("online checkpoint fault not reached: %d %v", st, b)
	}
	if _, e = pool.Exec(context.Background(), `DROP TRIGGER reject_online_checkpoint ON accounts; DROP FUNCTION reject_online_checkpoint()`); e != nil {
		t.Fatal(e)
	}
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/connect", token, `{}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	mu.Lock()
	same := len(ids) == 2 && ids[0] != "" && ids[0] == ids[1]
	mu.Unlock()
	if !same {
		t.Fatal("commit failure lost connection intent")
	}
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/transition", token, `{"to":"idle","expectedFrom":"online"}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	mu.Lock()
	ended = true
	mu.Unlock()
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/connect", token, `{}`)
	if st != 409 || b["error"].(map[string]any)["code"] != "CONNECTION_ENDED" {
		t.Fatalf("ended incarnation: %d %v", st, b)
	}
	mu.Lock()
	ended = false
	mu.Unlock()
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/connect", token, `{}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	mu.Lock()
	defer mu.Unlock()
	if ids[2] == ids[3] || ids[2] == ids[0] {
		t.Fatalf("ended intent replayed instead of new explicit connect: %v", ids)
	}
}

func TestOrdinaryDisconnectIsDurableAndStaleIncarnationsCannotOverride(t *testing.T) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	var mu sync.Mutex
	latest := ""
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/connect") {
			var b struct {
				ConnectionID string `json:"connectionId"`
			}
			json.NewDecoder(r.Body).Decode(&b)
			mu.Lock()
			latest = b.ConnectionID
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"platformUserId": "p-account-1", "connectionId": b.ConnectionID})
			return
		}
		base.ServeHTTP(w, r)
	})
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	if e := a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	token, _ := login(t, s, "admin")
	st, b, _ := call(t, s, "POST", "/api/accounts/account-1/connect", token, `{}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	mu.Lock()
	first := latest
	mu.Unlock()
	if first == "" {
		t.Fatal("gateway connect lacks durable incarnation")
	}
	g.event("account_status", map[string]any{"accountId": "account-1", "status": "offline", "connectionId": first})
	eventually(t, 3*time.Second, func() bool { return accountStatus(t, s, token, "account-1") == "disconnected" })
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/connect", token, `{}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	mu.Lock()
	second := latest
	mu.Unlock()
	if second == first {
		t.Fatal("ended incarnation reused")
	}
	a.Close()
	cmd, restored := childServer(t, cfg)
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	g.event("account_status", map[string]any{"accountId": "account-1", "status": "offline", "connectionId": first})
	time.Sleep(1500 * time.Millisecond)
	if accountStatus(t, restored, token, "account-1") != "online" {
		t.Fatal("stale disconnect beat new connection")
	}
	st, b, _ = call(t, restored, "POST", "/api/accounts/account-1/transition", token, `{"to":"idle","expectedFrom":"online"}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	g.event("account_status", map[string]any{"accountId": "account-1", "status": "offline", "connectionId": second})
	time.Sleep(1500 * time.Millisecond)
	if accountStatus(t, restored, token, "account-1") != "idle" {
		t.Fatal("disconnect beat explicit offline")
	}
	st, b, _ = call(t, restored, "POST", "/api/accounts/account-1/transition", token, `{"to":"session_expired","expectedFrom":"idle"}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	g.event("account_status", map[string]any{"accountId": "account-1", "status": "offline", "connectionId": second})
	g.event("account_status", map[string]any{"accountId": "account-2", "status": "suspended"})
	eventually(t, 3*time.Second, func() bool { return accountStatus(t, restored, token, "account-2") == "suspended" })
	if accountStatus(t, restored, token, "account-1") != "session_expired" {
		t.Fatal("terminal was revived")
	}
}
