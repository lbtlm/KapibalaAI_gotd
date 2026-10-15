package gateway

import "testing"

// These are state-transition inputs, not platform clients or Telegram responses.
func TestOrdinaryOfflineRejectsStaleGenerationAndTerminalRevival(t *testing.T) {
	s := &Service{state: initialState()}
	s.state.Accounts["a"] = &account{Status: "online", PlatformID: "123", ConnectionID: "new", EndedConnections: map[string]bool{"old": true}}
	if s.ordinaryOffline("a", "old") || len(s.state.Events) != 0 || s.state.Accounts["a"].ConnectionID != "new" {
		t.Fatal("stale disconnect modified the new generation")
	}
	if !s.ordinaryOffline("a", "new") || len(s.state.Events) != 1 {
		t.Fatal("current disconnect was not emitted exactly once")
	}
	a := s.state.Accounts["a"]
	if a.Status != "offline" || a.ConnectionID != "" || !a.EndedConnections["new"] || s.state.Events[0].Data["connectionId"] != "new" {
		t.Fatal("current generation was not ended durably in state")
	}
	if s.ordinaryOffline("a", "new") || len(s.state.Events) != 1 {
		t.Fatal("ended generation was emitted twice")
	}
	a.Status, a.ConnectionID = "suspended", "terminal"
	if s.ordinaryOffline("a", "terminal") || a.Status != "suspended" {
		t.Fatal("terminal status was revived")
	}
}

func TestTerminalTransitionRemovesMembershipAndEndsConnection(t *testing.T) {
	s := &Service{state: initialState()}
	s.state.Accounts["a"] = &account{Status: "online", PlatformID: "123", ConnectionID: "current"}
	s.state.Groups["g"] = &group{Members: map[string]bool{"123": true, "456": true}}
	s.terminal("a", "session_expired")
	a := s.state.Accounts["a"]
	if a.Status != "session_expired" || a.ConnectionID != "" || !a.EndedConnections["current"] || s.state.Groups["g"].Members["123"] || !s.state.Groups["g"].Members["456"] || len(s.state.Events) != 2 {
		t.Fatal("terminal transition failed to preserve account/membership invariants")
	}
}
