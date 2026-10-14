package gateway_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGatewayCrashChild(t *testing.T) {
	if os.Getenv("GATEWAY_CRASH_CHILD") != "1" {
		t.Skip("subprocess helper only; GATEWAY_CRASH_CHILD=1")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, e := gateway.OpenPostgres(ctx, os.Getenv("DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	s, e := gateway.New(ctx, gateway.Config{Mode: "fake", Store: store, AdminEnabled: true})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Handler: s, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	if e = os.WriteFile(os.Getenv("GATEWAY_CRASH_ADDRESS_FILE"), []byte("http://"+listener.Addr().String()), 0600); e != nil {
		t.Fatal(e)
	}
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	select {
	case <-ctx.Done():
	case e = <-done:
		if e != http.ErrServerClosed {
			t.Fatal(e)
		}
	}
	_ = server.Close()
}

type ownedGatewayChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	base   string
	output bytes.Buffer
}

func startGatewayChild(t *testing.T, dsn string) *ownedGatewayChild {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	address := filepath.Join(t.TempDir(), "address.txt")
	child := &ownedGatewayChild{}
	child.cmd = exec.Command(exe, "-test.run=^TestGatewayCrashChild$")
	child.cmd.Env = append(os.Environ(), "GATEWAY_CRASH_CHILD=1", "DATABASE_URL="+dsn, "GATEWAY_CRASH_ADDRESS_FILE="+address)
	child.cmd.Stdout = &child.output
	child.cmd.Stderr = &child.output
	child.stdin, e = child.cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = child.cmd.Start(); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(address); e == nil {
			child.base = string(b)
			res, e := http.Get(child.base + "/health")
			if e == nil {
				res.Body.Close()
				if res.StatusCode == 200 {
					return child
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = child.cmd.Process.Kill()
	_ = child.cmd.Wait()
	t.Fatal("owned fake child startup failed")
	return nil
}
func stopGatewayChild(t *testing.T, c *ownedGatewayChild) {
	t.Helper()
	if c.cmd.ProcessState != nil {
		return
	}
	_ = c.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case e := <-done:
		if e != nil {
			t.Errorf("owned child graceful cleanup: %v", e)
		}
	case <-time.After(3 * time.Second):
		_ = c.cmd.Process.Kill()
		<-done
		t.Error("owned child failed graceful cleanup")
	}
}
func childRequest(t *testing.T, c *ownedGatewayChild, method, path string, in any) (int, map[string]any) {
	t.Helper()
	var b bytes.Buffer
	if in != nil {
		_ = json.NewEncoder(&b).Encode(in)
	}
	req, _ := http.NewRequest(method, c.base+path, &b)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var data map[string]any
	_ = json.NewDecoder(res.Body).Decode(&data)
	return res.StatusCode, data
}
func crashDatabase(t *testing.T) string {
	t.Helper()
	base := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required for process crash integration")
	}
	parsed, e := url.Parse(base)
	if e != nil {
		t.Fatal(e)
	}
	admin, e := sql.Open("pgx", base)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	_, e = admin.Exec(`CREATE DATABASE kapibala_gateway_crash`)
	if e != nil && !strings.Contains(e.Error(), "42P04") {
		t.Fatal("create dedicated crash database", e)
	}
	parsed.Path = "/kapibala_gateway_crash"
	return parsed.String()
}
func TestGatewayHardKillRetainsAcceptedEffectsAndSSEHistory(t *testing.T) {
	dsn := crashDatabase(t)
	first := startGatewayChild(t, dsn)
	t.Cleanup(func() { stopGatewayChild(t, first) })
	for _, path := range []string{"/admin/reset", "/accounts/account-1/connect"} {
		status, _ := childRequest(t, first, "POST", path, nil)
		if status != 200 {
			t.Fatal(status)
		}
	}
	status, body := childRequest(t, first, "POST", "/groups", map[string]string{"creatorAccountId": "account-1", "idempotencyKey": "process-create"})
	if status != 200 {
		t.Fatal(status)
	}
	gid := body["groupId"].(string)
	status, _ = childRequest(t, first, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "settled", "text": "settled-before-crash"})
	if status != 202 {
		t.Fatal(status)
	}
	deadline := time.Now().Add(2 * time.Second)
	var original map[string]any
	for {
		status, original = childRequest(t, first, "GET", "/groups/"+gid+"/messages/by-client-id/settled", nil)
		if status == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first message did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, _ = childRequest(t, first, "POST", "/admin/faults", map[string]any{"operation": "send", "code": "NETWORK_TIMEOUT", "effect": true, "settleAfterMs": 1500})
	if status != 200 {
		t.Fatal(status)
	}
	status, _ = childRequest(t, first, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "pending", "text": "pending-after-crash"})
	if status != 504 {
		t.Fatal(status)
	}
	if e := first.cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e := first.cmd.Wait(); e == nil || first.cmd.ProcessState.ExitCode() == 0 {
		t.Fatal("expected real OS hard kill")
	}
	second := startGatewayChild(t, dsn)
	t.Cleanup(func() { stopGatewayChild(t, second) })
	deadline = time.Now().Add(4 * time.Second)
	var recovered map[string]any
	for {
		status, recovered = childRequest(t, second, "GET", "/groups/"+gid+"/messages/by-client-id/pending", nil)
		if status == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("accepted effect lost after OS kill", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, again := childRequest(t, second, "GET", "/groups/"+gid+"/messages/by-client-id/settled", nil)
	if status != 200 || again["msgId"] != original["msgId"] || again["sentAt"] != original["sentAt"] {
		t.Fatal("old message changed after process reconstruction")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", second.base+"/events?since=0", nil)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	scan := bufio.NewScanner(res.Body)
	counts := map[string]int{}
	ids := map[string]bool{}
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		_ = json.Unmarshal([]byte(line[6:]), &event)
		kind := event["type"].(string)
		if kind == "member_joined" {
			t.Fatal("creator joined event unexpectedly appeared")
		}
		counts[kind]++
		id := event["msgId"].(string)
		ids[id] = true
		if counts["message_sent"] == 2 && counts["message"] == 2 {
			break
		}
	}
	if counts["message_sent"] != 2 || counts["message"] != 2 || len(ids) != 2 || !ids[recovered["msgId"].(string)] {
		t.Fatalf("history/effect duplicates or loss: %v %v", counts, ids)
	}
	t.Log("one owned fake child OS-killed; replacement replayed two distinct messages and retained accepted pending effect")
}
