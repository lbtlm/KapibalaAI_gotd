package platform_test

import (
	"sync"
	"testing"
)

func TestSequencePreflightRejectsWithoutRunningAndConcurrentStartHasOneWinner(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	token, _ := login(t, s, "admin")

	st, b, _ := call(t, s, "POST", "/api/sequences", token, `{"name":"test","steps":[{"index":1,"accountRole":"admin","text":"{event}","delaySeconds":1},{"index":2,"accountRole":"member","text":"{event}","delaySeconds":1},{"index":3,"accountRole":"member","text":"{missing}","delaySeconds":1}]}`)
	if st != 200 && st != 201 {
		t.Fatalf("create sequence %d %v", st, b)
	}
	seq := b["id"].(string)
	st, b, _ = call(t, s, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"`+seq+`","vars":{"event":"hello"}}`)
	if st != 422 || b["error"].(map[string]any)["stepIndex"] != float64(3) || b["error"].(map[string]any)["code"] != "UNRESOLVED_PLACEHOLDER" || b["error"].(map[string]any)["key"] != "missing" {
		t.Fatalf("preflight %d %v", st, b)
	}
	_, detail, _ := call(t, s, "GET", "/api/groups/"+group, token, "")
	_, timeline, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
	if detail["activeSequenceRunId"] != nil || len(timeline["items"].([]any)) != 0 {
		t.Fatal("failed preflight left an active run or outgoing message")
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, body, _ := call(t, s, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"`+seq+`","vars":{"event":"hello","missing":"done"}}`)
			if st == 409 && body["error"].(map[string]any)["code"] != "SEQUENCE_ALREADY_RUNNING" {
				t.Errorf("concurrent sequence start returned wrong error: %v", body)
			}
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
