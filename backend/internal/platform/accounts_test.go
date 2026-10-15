package platform_test

import (
	"sync"
	"testing"
)

func TestConcurrentAccountCASHasOneWinnerAndTerminalCannotReconnect(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	insertAccountRows(t, cfg, "account-1")
	token, _ := login(t, s, "admin")
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _, _ := call(t, s, "POST", "/api/accounts/account-1/transition", token, `{"to":"suspended","expectedFrom":"idle"}`)
			statuses <- status
		}()
	}
	wg.Wait()
	close(statuses)
	ok, conflict := 0, 0
	for st := range statuses {
		if st == 200 {
			ok++
		}
		if st == 409 {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("expected one CAS winner: %d/%d", ok, conflict)
	}
	st, body, _ := call(t, s, "POST", "/api/accounts/account-1/connect", token, "{}")
	if st != 409 || body["error"].(map[string]any)["code"] != "ILLEGAL_TRANSITION" {
		t.Fatalf("terminal reconnected: %d %v", st, body)
	}
}
