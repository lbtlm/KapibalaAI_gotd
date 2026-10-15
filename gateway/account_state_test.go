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

func TestTerminalTransitionIgnoresRepeatedAndConflictingTerminalSignals(t *testing.T) {
	for _, first := range []struct {
		status, later, code string
		httpStatus          int
	}{{"session_expired", "suspended", "SESSION_EXPIRED", 401}, {"suspended", "session_expired", "ACCOUNT_SUSPENDED", 403}} {
		t.Run(first.status, func(t *testing.T) {
			s := &Service{state: initialState()}
			s.state.Accounts["a"] = &account{Status: "online", PlatformID: "123", ConnectionID: "current"}
			s.state.Groups["g"] = &group{Members: map[string]bool{"123": true, "456": true}}
			s.terminal("a", first.status)
			if len(s.state.Events) != 2 || s.state.Groups["g"].Members["123"] || !s.state.Groups["g"].Members["456"] || !s.state.Accounts["a"].EndedConnections["current"] {
				t.Fatal("first permanent signal lost its membership/connection cleanup")
			}
			s.terminal("a", first.status)
			s.terminal("a", first.later)
			if s.state.Accounts["a"].Status != first.status || len(s.state.Events) != 2 || s.state.Events[0].Data["status"] != first.status {
				t.Fatal("late terminal signals changed the permanent reason or emitted another transition")
			}
			for _, online := range []bool{false, true} {
				err, ok := s.accountError("a", online).(*APIError)
				if !ok || err.Code != first.code || err.Status != first.httpStatus {
					t.Fatal("later account requests lost the specific permanent error")
				}
			}
		})
	}
}
