package platform_test

import (
	"context"
	"github.com/gorilla/websocket"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type databaseProxy struct {
	listener net.Listener
	target   string
	mu       sync.Mutex
	down     bool
	conns    map[net.Conn]bool
	wg       sync.WaitGroup
}

func newDatabaseProxy(t *testing.T, dsn string) *databaseProxy {
	t.Helper()
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &databaseProxy{listener: l, target: u.Host, conns: map[net.Conn]bool{}}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			p.mu.Lock()
			down := p.down
			if !down {
				p.conns[c] = true
			}
			p.mu.Unlock()
			if down {
				c.Close()
				continue
			}
			p.wg.Add(1)
			go func(c net.Conn) {
				defer p.wg.Done()
				defer c.Close()
				defer func() { p.mu.Lock(); delete(p.conns, c); p.mu.Unlock() }()
				up, e := net.Dial("tcp", p.target)
				if e != nil {
					return
				}
				defer up.Close()
				done := make(chan struct{})
				go func() { io.Copy(up, c); up.Close(); close(done) }()
				io.Copy(c, up)
				c.Close()
				<-done
			}(c)
		}
	}()
	t.Cleanup(func() { l.Close(); p.setDown(true); p.wg.Wait() })
	return p
}
func (p *databaseProxy) setDown(down bool) {
	p.mu.Lock()
	p.down = down
	if down {
		for c := range p.conns {
			c.Close()
		}
	}
	p.mu.Unlock()
}
func TestAllDatabaseConnectionsLostReplayGatewayContentAndInconsistency(t *testing.T) {
	g := gatewayFixture(t)
	original, old, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	original.Start(context.Background())
	token, _ := login(t, old, "admin")
	group := createGroup(t, old, token)
	original.Close()
	proxy := newDatabaseProxy(t, cfg.DatabaseURL)
	u, _ := url.Parse(cfg.DatabaseURL)
	u.Host = proxy.listener.Addr().String()
	cfg.DatabaseURL = u.String()
	a, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	s := httptest.NewServer(a.Handler())
	defer s.Close()
	defer a.Close()
	a.Start(context.Background())
	time.Sleep(200 * time.Millisecond)
	proxy.setDown(true)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "outage-message", "senderPlatformUserId": "outside", "text": "retained during outage", "sentAt": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)})
	time.Sleep(500 * time.Millisecond)
	proxy.setDown(false)
	eventually(t, 3*time.Second, func() bool {
		st, v, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
		if st != 200 {
			return false
		}
		items, _ := v["items"].([]any)
		return len(items) == 1 && items[0].(map[string]any)["msgId"] == "outage-message"
	})
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/ws", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	c.WriteJSON(map[string]any{"type": "auth", "accessToken": token, "sinceSeq": 0})
	var frame map[string]any
	c.ReadJSON(&frame)
	alert := false
	for i := 0; i < 100; i++ {
		if e = c.ReadJSON(&frame); e != nil {
			break
		}
		if frame["type"] == "inconsistency" {
			alert = true
			break
		}
	}
	if !alert {
		t.Fatal("database outage notice was not persisted for websocket replay")
	}
}
