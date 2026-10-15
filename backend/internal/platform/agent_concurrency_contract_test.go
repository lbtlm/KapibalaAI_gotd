package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestAgentTwoInstancesSerializeRunsAndCollectPendingExternalMessages(t *testing.T) {
	var calls, active, peak atomic.Int32
	turns := make(chan domain.TurnRequest, 4)
	first, second := make(chan struct{}), make(chan struct{})
	var releaseFirst, releaseSecond sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var turn domain.TurnRequest
		if err := json.NewDecoder(r.Body).Decode(&turn); err != nil {
			t.Error(err)
			return
		}
		n := calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		turns <- turn
		gate := first
		if n != 1 {
			gate = second
		}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"batch completed"}]}`)
	}))
	defer provider.Close()
	defer releaseFirst.Do(func() { close(first) })
	defer releaseSecond.Do(func() { close(second) })
	a, server, cfg := fixtureConfig(t, platform.Config{AgentURL: provider.URL})
	group := insertAgentInput(t, cfg)
	token, _ := login(t, server, "viewer")
	peer, err := platform.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err = a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = peer.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	initial := nextAgentTurn(t, turns)
	for i, item := range []struct{ id, sender, at string }{
		{"later", "outside", "2026-01-02T00:00:00Z"},
		{"earlier", "outside", "2026-01-01T00:00:00Z"},
		{"manual", "database-input-c", "2026-01-03T00:00:00Z"},
	} {
		putInbox(t, cfg, i+1, "message", map[string]any{"groupId": "unroutable-database-group", "msgId": item.id, "senderPlatformUserId": item.sender, "text": "pending input", "sentAt": item.at})
	}
	eventually(t, 3*time.Second, func() bool {
		_, timeline, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
		return len(timeline["items"].([]any)) == 4
	})
	if runs := runList(t, server, token, group); len(runs) != 1 || runs[0]["status"] != "running" || calls.Load() != 1 {
		t.Fatalf("second instance started an overlapping run: %v calls=%d", runs, calls.Load())
	}
	releaseFirst.Do(func() { close(first) })
	next := nextAgentTurn(t, turns)
	var trigger struct {
		TriggerMessages []struct {
			MsgID string `json:"msgId"`
		} `json:"triggerMessages"`
	}
	if err = json.Unmarshal([]byte(next.Messages[0].Content[0].Text), &trigger); err != nil {
		t.Fatal(err)
	}
	if initial.RunID == next.RunID || len(trigger.TriggerMessages) != 2 || trigger.TriggerMessages[0].MsgID != "earlier" || trigger.TriggerMessages[1].MsgID != "later" {
		t.Fatalf("pending batch lost, duplicated or unsorted: %v", trigger)
	}
	releaseSecond.Do(func() { close(second) })
	eventually(t, 3*time.Second, func() bool {
		runs := runList(t, server, token, group)
		return len(runs) == 2 && runs[0]["status"] == "finished" && runs[1]["status"] == "finished"
	})
	if calls.Load() != 2 || peak.Load() != 1 {
		t.Fatalf("turn overlap or extra run: calls=%d peak=%d", calls.Load(), peak.Load())
	}
}

func nextAgentTurn(t *testing.T, turns <-chan domain.TurnRequest) domain.TurnRequest {
	t.Helper()
	select {
	case turn := <-turns:
		return turn
	case <-time.After(4 * time.Second):
		t.Fatal("Agent turn did not arrive")
		return domain.TurnRequest{}
	}
}
