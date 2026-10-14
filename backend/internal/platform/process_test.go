package platform_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServerProcessHelper(t *testing.T) {
	if os.Getenv("KAPIBALA_CHILD_SERVER") != "true" {
		t.Skip("subprocess helper")
	}
	cfg := platform.Config{DatabaseURL: os.Getenv("KAPIBALA_CHILD_DSN"), GatewayURL: os.Getenv("KAPIBALA_CHILD_GATEWAY"), AgentURL: os.Getenv("KAPIBALA_CHILD_AGENT")}
	a, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.Start(context.Background())
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	fmt.Printf("CHILD_URL=http://%s\n", l.Addr())
	if e = http.Serve(l, a.Handler()); e != nil {
		t.Fatal(e)
	}
}
func childServer(t *testing.T, cfg platform.Config) (*exec.Cmd, *httptest.Server) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestServerProcessHelper$", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), "KAPIBALA_CHILD_SERVER=true", "KAPIBALA_CHILD_DSN="+cfg.DatabaseURL, "KAPIBALA_CHILD_GATEWAY="+cfg.GatewayURL, "KAPIBALA_CHILD_AGENT="+cfg.AgentURL)
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(out)
	var base string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "CHILD_URL=") {
			base = strings.TrimPrefix(scanner.Text(), "CHILD_URL=")
			break
		}
	}
	if base == "" {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("child did not start")
	}
	return cmd, &httptest.Server{URL: base}
}
func TestHardProcessKillAfterGatewayEffectRestoresSameAgentRunWithoutRepeatingSend(t *testing.T) {
	g := gatewayFixture(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	defer close(release)
	base := g.server.Config.Handler
	var sends, audits atomic.Int32
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/groups/gateway-group/send" {
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			sends.Add(1)
			sent := time.Now().UTC().Format(time.RFC3339Nano)
			g.mu.Lock()
			g.settled[b["clientMsgId"]] = map[string]any{"msgId": "hard-kill-msg", "sentAt": sent}
			g.mu.Unlock()
			g.event("message_sent", map[string]any{"clientMsgId": b["clientMsgId"], "msgId": "hard-kill-msg", "sentAt": sent})
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
				return
			case <-release:
				w.WriteHeader(202)
				fmt.Fprint(w, `{"accepted":true}`)
				return
			}
		}
		base.ServeHTTP(w, r)
	})
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"pass"}`)
			return
		}
		var req domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) == 1 {
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"send-1","name":"send_message","input":{"text":"hard kill test","idempotency_key":"logical-send"}}]}`)
		} else {
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"restored"}]}`)
		}
	}))
	defer agent.Close()
	a, _, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
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
	group := createGroup(t, s, token)
	call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "hard-trigger", "senderPlatformUserId": "outside", "text": "trigger", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	select {
	case <-entered:
	case <-time.After(8 * time.Second):
		t.Fatal("external send not reached")
	}
	runs := runList(t, s, token, group)
	runID := runs[0]["id"].(string)
	if e := cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	cmd.Wait()
	alive = false
	restarted, server := childServer(t, cfg)
	defer func() { restarted.Process.Kill(); restarted.Wait() }()
	eventually(t, 5*time.Second, func() bool {
		runs := runList(t, server, token, group)
		return len(runs) == 1 && runs[0]["id"] == runID && runs[0]["status"] == "finished"
	})
	eventually(t, 3*time.Second, func() bool {
		_, body, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
		for _, item := range body["items"].([]any) {
			m := item.(map[string]any)
			if m["msgId"] == "hard-kill-msg" {
				return m["deliveryStatus"] == "sent"
			}
		}
		return false
	})
	if sends.Load() != 1 || audits.Load() != 1 {
		t.Fatalf("replayed external effects %d/%d", sends.Load(), audits.Load())
	}
}
