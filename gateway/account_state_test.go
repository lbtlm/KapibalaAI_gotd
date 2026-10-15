package gateway

import (
	"context"
	"testing"
)

func TestDuplicateConnectCannotFinalizeOwnersFailedAttempt(t *testing.T) {
	for _, code := range []struct {
		code, status string
		httpStatus   int
	}{{"AUTH_KEY_DUPLICATED", "session_expired", 401}, {"SESSION_EXPIRED", "session_expired", 401}, {"ACCOUNT_SUSPENDED", "suspended", 403}, {"NETWORK_TIMEOUT", "idle", 504}} {
		t.Run(code.code, func(t *testing.T) {
			// A's Run has ended, but A is paused before taking Service.mu to
			// classify its result. B reaches the actual adapter first.
			done := make(chan struct{})
			close(done)
			adapter := &TelegramAdapter{sessions: map[string]*onlineSession{"a": {connectionID: "same", done: done}}}
			s := &Service{state: initialState()}
			s.state.Accounts["a"] = &account{Status: "idle", ConnectionID: "same"}
			_, duplicateErr := adapter.Connect(context.WithValue(context.Background(), connectionIDKey{}, "same"), "a")
			if e := platformError(duplicateErr); e == nil || e.Code != "CONNECTION_ENDED" {
				t.Fatal("duplicate did not observe the ended adapter attempt")
			}
			s.mu.Lock()
			if failedConnectionStopped(duplicateErr) {
				s.finishFailedConnection("a", "same")
			}
			s.mu.Unlock()
			if s.state.Accounts["a"].ConnectionID != "same" {
				t.Fatal("duplicate finalized the owner's CID before its result was classified")
			}
			// A now wins the service lock and retains its original platform result.
			s.mu.Lock()
			ownerErr := &connectionAttemptError{err: &APIError{code.httpStatus, code.code, 0}, done: done}
			failure := s.accountFailure("a", ownerErr)
			if failedConnectionStopped(ownerErr) {
				s.finishFailedConnection("a", "same")
			}
			s.mu.Unlock()
			if e := platformError(failure); e.Code != code.code || s.state.Accounts["a"].Status != code.status || s.state.Accounts["a"].ConnectionID != "" {
				t.Fatal("owner lost its concrete error or final lifecycle state")
			}
		})
	}
}

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

func TestAccountDeletionRejectsOccupiedStatesAndUnresolvedEffects(t *testing.T) {
	for _, status := range []string{"idle", "offline", "session_expired", "suspended", "online", "rate_limited", "connecting", "unknown"} {
		t.Run(status, func(t *testing.T) {
			s := &Service{state: initialState()}
			s.state.Accounts["a"] = &account{Status: status, PlatformID: "123"}
			want := "active_connection"
			if status == "idle" || status == "offline" || status == "session_expired" || status == "suspended" {
				want = ""
			}
			if got := s.deletionBlocker("a"); got != want {
				t.Fatalf("status %s deletion reason %q, want %q", status, got, want)
			}
		})
	}
	s := &Service{state: initialState()}
	s.state.Accounts["a"] = &account{Status: "offline"}
	for _, stage := range []string{"prepared", "unknown", "accepted", "sent", "completed", "failed"} {
		for _, promotion := range []bool{false, true} {
			key, actor, target := "send", "a", ""
			if promotion {
				key, actor, target = "promote:key", "other", "a"
			}
			s.state.Effects = map[string]*effect{key: {AccountID: actor, TargetID: target, State: stage}}
			want := "active_effect"
			if stage == "sent" || stage == "completed" || stage == "failed" {
				want = ""
			}
			if s.deletionBlocker("a") != want {
				t.Fatalf("effect %s promotion=%t lost deletion fence", stage, promotion)
			}
		}
	}
	s.state.Effects = map[string]*effect{"kick:key": {AccountID: "other", TargetID: "a", State: "unknown"}}
	if s.deletionBlocker("a") != "" {
		t.Fatal("kick platform ID was mistaken for an account ID")
	}
}

func TestAccountDeletionRejectsConnectionIntentWithoutAdapterSlot(t *testing.T) {
	for _, status := range []string{"idle", "offline"} {
		t.Run(status, func(t *testing.T) {
			s := &Service{state: initialState()}
			s.state.Accounts["a"] = &account{Status: status, ConnectionID: "persisted-before-adapter"}
			if got := s.deletionBlocker("a"); got != "active_connection" {
				t.Fatal("a persisted connection intent required a client slot to prevent deletion")
			}
		})
	}
}

func TestFailedConnectionCleanupPreservesSpecificErrorAndFencesItsGeneration(t *testing.T) {
	for _, code := range []struct {
		code   string
		status int
	}{{"NETWORK_TIMEOUT", 504}, {"GATEWAY_UNAVAILABLE", 503}, {"SESSION_EXPIRED", 401}, {"ACCOUNT_SUSPENDED", 403}} {
		t.Run(code.code, func(t *testing.T) {
			s := &Service{state: initialState()}
			s.state.Accounts["a"] = &account{Status: "idle", ConnectionID: "failed"}
			done := make(chan struct{})
			failure := &connectionAttemptError{err: &APIError{code.status, code.code, 0}, done: done}
			if failedConnectionStopped(failure) {
				t.Fatal("error code falsely proved a running client stopped")
			}
			close(done)
			if !failedConnectionStopped(failure) {
				t.Fatal("stopped Run did not permit cleanup")
			}
			out := s.accountFailure("a", failure)
			s.finishFailedConnection("a", "failed")
			if e := platformError(out); e.Code != code.code || e.Status != code.status {
				t.Fatal("intent cleanup masked the original platform error")
			}
			if s.state.Accounts["a"].ConnectionID != "" || !s.state.Accounts["a"].EndedConnections["failed"] || len(s.state.Events) != 1 {
				t.Fatal("failed generation did not converge exactly once")
			}
			if code.status >= 500 && s.state.Accounts["a"].Status != "idle" {
				t.Fatal("temporary failure changed the initial idle state")
			}
			if s.finishFailedConnection("a", "failed") || len(s.state.Events) != 1 {
				t.Fatal("failed connection cleanup emitted twice")
			}
		})
	}
	for _, status := range []string{"idle", "offline", "online", "session_expired", "suspended"} {
		s := &Service{state: initialState()}
		s.state.Accounts["a"] = &account{Status: status, ConnectionID: "new"}
		if s.finishFailedConnection("a", "old") || len(s.state.Events) != 0 || s.state.Accounts["a"].ConnectionID != "new" {
			t.Fatal("stale failed attempt ended a newer generation")
		}
		want := status == "idle" || status == "offline"
		if s.finishFailedConnection("a", "new") != want || s.state.Accounts["a"].Status != status {
			t.Fatal("cleanup changed a live/terminal status")
		}
	}
}

func TestAccountDeletionRequiresConfirmedLeaveAndJoinRestoresOccupancy(t *testing.T) {
	s := &Service{state: initialState()}
	s.state.Accounts["a"] = &account{Status: "online", PlatformID: "123", ConnectionID: "current"}
	g := &group{Owner: "a", Members: map[string]bool{"123": true}, AccessHashes: map[string]int64{"a": 1}}
	s.state.Groups["g"] = g
	s.terminal("a", "session_expired")
	if g.Members["123"] || s.deletionBlocker("a") != "managed_group" {
		t.Fatal("terminal membership cleanup was mistaken for a confirmed leave")
	}
	g.confirmAccountLeave("a")
	if s.deletionBlocker("a") != "" {
		t.Fatal("confirmed historical leave kept account occupied")
	}
	g.Recovery = &HistoryRecovery{AccountID: "a", Status: "pending"}
	if s.deletionBlocker("a") != "active_effect" {
		t.Fatal("confirmed leave bypassed an active history recovery")
	}
	g.Recovery.Status = "recovered_with_limits"
	g.confirmAccountJoin("a")
	if g.LeftAccounts["a"] || s.deletionBlocker("a") != "managed_group" {
		t.Fatal("confirmed join did not restore managed occupancy")
	}
	g.confirmAccountLeave("a")
	g.Members["123"] = true
	g.confirmObservedJoins(s.state.Accounts)
	s.terminal("a", "session_expired")
	if g.LeftAccounts["a"] || s.deletionBlocker("a") != "managed_group" {
		t.Fatal("a confirmed membership followed by a terminal signal revived an obsolete leave")
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
