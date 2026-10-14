package platform_test

import (
	"context"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSequenceRestartReschedulesOnlyFirstExpiredStepThenWaitsForPreviousSent(t *testing.T) {
	g := gatewayFixture(t)
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	a.Close()
	// Reopen an HTTP-only process to create an already-due run without workers.
	idle, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(idle.Handler())
	st, b, _ := call(t, server, "POST", "/api/sequences", token, `{"name":"timed","steps":[{"index":1,"accountRole":"admin","text":"first","delaySeconds":0.3},{"index":2,"accountRole":"member","text":"second","delaySeconds":0.3}]}`)
	if st != 201 {
		t.Fatalf("sequence %d %v", st, b)
	}
	seq := b["id"].(string)
	st, b, _ = call(t, server, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"`+seq+`"}`)
	if st != 201 {
		t.Fatal(st)
	}
	run := b["runId"].(string)
	server.Close()
	idle.Close()
	time.Sleep(400 * time.Millisecond)
	restored, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	server = httptest.NewServer(restored.Handler())
	defer server.Close()
	start := time.Now()
	restored.Start(context.Background())
	time.Sleep(150 * time.Millisecond)
	g.mu.Lock()
	calls := g.sendCount
	g.mu.Unlock()
	if calls != 0 {
		t.Fatal("expired step fired immediately on restart")
	}
	deadline := time.Now().Add(5 * time.Second)
	var latest map[string]any
	for time.Now().Before(deadline) {
		_, latest, _ = call(t, server, "GET", "/api/sequence-runs/"+run, token, "")
		if latest["status"] == "finished" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if latest["status"] != "finished" {
		_, messages, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
		t.Fatalf("sequence did not finish: %v messages=%v", latest, messages)
	}
	_, detail, _ := call(t, server, "GET", "/api/sequence-runs/"+run, token, "")
	steps := detail["steps"].([]any)
	first := steps[0].(map[string]any)
	second := steps[1].(map[string]any)
	firstScheduled, e := time.Parse(time.RFC3339Nano, first["scheduledAt"].(string))
	if e != nil {
		t.Fatal(e)
	}
	firstSent, e := time.Parse(time.RFC3339Nano, first["sentAt"].(string))
	if e != nil {
		t.Fatal(e)
	}
	secondScheduled, e := time.Parse(time.RFC3339Nano, second["scheduledAt"].(string))
	if e != nil {
		t.Fatal(e)
	}
	if firstScheduled.Sub(start) < 250*time.Millisecond || secondScheduled.Sub(firstSent) < 299*time.Millisecond {
		t.Fatalf("incorrect schedule restart=%v afterSent=%v", firstScheduled.Sub(start), secondScheduled.Sub(firstSent))
	}
}
