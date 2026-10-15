package platform_test

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"strings"
	"testing"
	"time"
)

func TestWebSocketAuthenticatedReplayHasCommittedMonotonicSequence(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	insertAccountRows(t, cfg, "account-1")
	token, _ := login(t, s, "admin")
	call(t, s, "POST", "/api/accounts/account-1/transition", token, `{"expectedFrom":"idle","to":"suspended"}`)
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/ws", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	c.WriteJSON(map[string]any{"type": "auth", "accessToken": token, "sinceSeq": 0})
	var frame map[string]any
	if e = c.ReadJSON(&frame); e != nil {
		t.Fatal(e)
	}
	if frame["success"] != true {
		t.Fatalf("auth %v", frame)
	}
	last := float64(0)
	for i := 0; i < 2; i++ {
		_, b, e := c.ReadMessage()
		if e != nil {
			t.Fatal(e)
		}
		json.Unmarshal(b, &frame)
		seq := frame["seq"].(float64)
		if seq <= last {
			t.Fatalf("seq went backwards %v", frame)
		}
		last = seq
	}
}
