package platform_test

import (
	"context"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"sync"
	"testing"
)

func TestSequencePreflightRejectsWithoutRunningAndConcurrentStartHasOneWinner(t *testing.T) {
	g := gatewayFixture(t)
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	st, b, _ := call(t, s, "POST", "/api/sequences", token, `{"name":"test","steps":[{"index":1,"accountRole":"admin","text":"{event}","delaySeconds":1},{"index":2,"accountRole":"member","text":"{event}","delaySeconds":1},{"index":3,"accountRole":"member","text":"{missing}","delaySeconds":1}]}`)
	if st != 200 && st != 201 {
		t.Fatalf("create sequence %d %v", st, b)
	}
	seq := b["id"].(string)
	st, b, _ = call(t, s, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"`+seq+`","vars":{"event":"hello"}}`)
	if st != 422 || b["error"].(map[string]any)["stepIndex"] != float64(3) {
		t.Fatalf("preflight %d %v", st, b)
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, _, _ := call(t, s, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"`+seq+`","vars":{"event":"hello","missing":"done"}}`)
			statuses <- st
		}()
	}
	wg.Wait()
	close(statuses)
	ok, conflict := 0, 0
	for st := range statuses {
		if st == 201 {
			ok++
		}
		if st == 409 {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("start %d/%d", ok, conflict)
	}
}
