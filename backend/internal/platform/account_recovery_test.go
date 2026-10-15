package platform_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestPendingDisconnectCannotOvertakeReconnectAcrossProcessReconstruction(t *testing.T) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	var unavailable, online atomic.Bool
	var disconnects atomic.Int32
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/account-1/connect" {
			online.Store(true)
		}
		if r.URL.Path == "/accounts/account-1/disconnect" {
			disconnects.Add(1)
			if unavailable.Load() {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"code":"GATEWAY_UNAVAILABLE"}`)
				return
			}
			online.Store(false)
		}
		base.ServeHTTP(w, r)
	})
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	token, _ := login(t, s, "admin")
	call(t, s, "POST", "/api/accounts/account-1/connect", token, "{}")
	unavailable.Store(true)
	st, b, _ := call(t, s, "POST", "/api/accounts/account-1/transition", token, `{"to":"disconnected","expectedFrom":"online"}`)
	if st != 200 {
		t.Fatalf("offline intent not accepted: %d %v", st, b)
	}
	unavailable.Store(false)
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/connect", token, "{}")
	if st != 200 || !online.Load() {
		t.Fatalf("reconnect failed: %d %v", st, b)
	}
	a.Close()
	cmd, restarted := childServer(t, cfg)
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	time.Sleep(350 * time.Millisecond)
	if accountStatus(t, restarted, token, "account-1") != "online" || !online.Load() {
		t.Fatalf("old offline action overtook reconnect: gatewayOnline=%v disconnects=%d", online.Load(), disconnects.Load())
	}
}

func TestReconnectWaitsForInflightDisconnectBeforeConnecting(t *testing.T) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	entered, release := make(chan struct{}), make(chan struct{})
	var connecting, connected atomic.Int32
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/account-1/connect" {
			connected.Add(1)
		}
		if r.URL.Path == "/accounts/account-1/disconnect" {
			close(entered)
			<-release
		}
		base.ServeHTTP(w, r)
	})
	_, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	token, _ := login(t, s, "admin")
	call(t, s, "POST", "/api/accounts/account-1/connect", token, "{}")
	done := make(chan struct{})
	go func() {
		defer close(done)
		call(t, s, "POST", "/api/accounts/account-1/transition", token, `{"to":"disconnected","expectedFrom":"online"}`)
	}()
	<-entered
	reconnected := make(chan struct{})
	go func() {
		defer close(reconnected)
		connecting.Store(1)
		call(t, s, "POST", "/api/accounts/account-1/connect", token, "{}")
	}()
	eventually(t, time.Second, func() bool { return connecting.Load() == 1 })
	time.Sleep(200 * time.Millisecond)
	beforeRelease := connected.Load()
	close(release)
	<-done
	<-reconnected
	if beforeRelease != 1 || connected.Load() != 2 || accountStatus(t, s, token, "account-1") != "online" {
		t.Fatalf("connect passed inflight disconnect: before=%d after=%d", beforeRelease, connected.Load())
	}
}

func TestInterruptedDisconnectFinishesBeforeReconnectAfterHardKill(t *testing.T) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	entered := make(chan struct{})
	var online atomic.Bool
	var disconnects atomic.Int32
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/account-1/connect" {
			online.Store(true)
		}
		if r.URL.Path == "/accounts/account-1/disconnect" {
			io.Copy(io.Discard, r.Body)
			online.Store(false)
			if disconnects.Add(1) == 1 {
				close(entered)
				<-r.Context().Done()
				return
			}
		}
		base.ServeHTTP(w, r)
	})
	a, _, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	a.Close()
	cmd, s := childServer(t, cfg)
	alive := true
	defer func() {
		if alive {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	token, _ := login(t, s, "admin")
	call(t, s, "POST", "/api/accounts/account-1/connect", token, "{}")
	done := make(chan struct{})
	go func() {
		defer close(done)
		req, _ := http.NewRequest("POST", s.URL+"/api/accounts/account-1/transition", strings.NewReader(`{"to":"disconnected","expectedFrom":"online"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		if res, e := http.DefaultClient.Do(req); e == nil {
			res.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect not reached")
	}
	cmd.Process.Kill()
	cmd.Wait()
	alive = false
	<-done
	restarted, server := childServer(t, cfg)
	defer func() { restarted.Process.Kill(); restarted.Wait() }()
	st, b, _ := call(t, server, "POST", "/api/accounts/account-1/connect", token, "{}")
	if st != 200 || !online.Load() || disconnects.Load() != 2 || accountStatus(t, server, token, "account-1") != "online" {
		t.Fatalf("unknown disconnect was discarded or overtook reconnect: %d %v online=%v attempts=%d", st, b, online.Load(), disconnects.Load())
	}
}
