package gateway

import (
	"net/http"
	"sort"
	"time"
)

func (s *Service) groupHTTP(w http.ResponseWriter, r *http.Request) {
	var in input
	if r.Method == "POST" {
		if err := decode(r, &in); err != nil {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
	}
	gid, op := r.PathValue("groupId"), r.PathValue("operation")
	if r.Method == "GET" {
		op = "members"
	}
	if gid == "" {
		op = "create"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	aid := in.AccountID
	if in.ByAccountID != "" {
		aid = in.ByAccountID
	}
	if op == "create" {
		aid = in.CreatorAccountID
	}
	if op != "members" && op != "invite" {
		if err := s.accountError(aid, true); err != nil {
			fail(w, err)
			return
		}
	}
	g := s.state.Groups[gid]
	if op != "create" && g == nil {
		fail(w, &APIError{404, "GROUP_NOT_FOUND", 0})
		return
	}
	f := s.consumeFault(op, aid, gid)
	if f != nil && f.Code != "NETWORK_TIMEOUT" && f.Code != "RATE_LIMITED" && f.Code != "ASYNC_FAILED" && f.Code != "JOIN_MISSING" && f.Code != "INVITE_DELAY" {
		if f.Code == "ACCOUNT_SUSPENDED" || f.Code == "SESSION_EXPIRED" {
			s.terminal(aid, map[bool]string{true: "suspended", false: "session_expired"}[f.Code == "ACCOUNT_SUSPENDED"])
		}
		if f.Code == "INVITE_EXPIRED" && g != nil {
			g.InviteExpired = true
		}
		s.commitHTTP(w, r, 0, nil, faultError(f))
		return
	}
	if s.cfg.Telegram != nil {
		out, err := s.cfg.Telegram.Execute(r.Context(), op, gid, in, s)
		status := 200
		if op == "join" || op == "send" {
			status = 202
		}
		s.commitHTTP(w, r, status, out, err)
		return
	}
	now := timeNow()
	switch op {
	case "create":
		if in.CreatorAccountID == "" {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		if existing := s.state.CreateKeys[in.IdempotencyKey]; in.IdempotencyKey != "" && existing != "" {
			respond(w, 200, map[string]string{"groupId": existing})
			return
		}
		if f != nil && f.Code == "NETWORK_TIMEOUT" && !f.Effect {
			s.commitHTTP(w, r, 0, nil, faultError(f))
			return
		}
		s.state.NextID++
		gid = "group-" + stringID(s.state.NextID)
		s.state.Groups[gid] = &group{Owner: aid, Members: map[string]bool{s.state.Accounts[aid].PlatformID: true}, Admins: map[string]bool{}, Writable: true, AccessHashes: map[string]int64{}}
		if in.IdempotencyKey != "" {
			s.state.CreateKeys[in.IdempotencyKey] = gid
		}
		if f != nil && f.Code == "NETWORK_TIMEOUT" {
			s.commitHTTP(w, r, 0, nil, faultError(f))
			return
		}
		s.commitHTTP(w, r, 200, map[string]string{"groupId": gid}, nil)
	case "members":
		members := make([]map[string]string, 0, len(g.Members))
		for id := range g.Members {
			role := "member"
			if owner := s.state.Accounts[g.Owner]; owner != nil && owner.PlatformID == id {
				role = "creator"
			}
			for aid := range g.Admins {
				if a := s.state.Accounts[aid]; a != nil && a.PlatformID == id {
					role = "admin"
				}
			}
			members = append(members, map[string]string{"platformUserId": id, "role": role})
		}
		sort.Slice(members, func(i, j int) bool { return members[i]["platformUserId"] < members[j]["platformUserId"] })
		respond(w, 200, members)
	case "invite":
		s.state.NextID++
		g.Invite = "https://t.me/+fake-" + stringID(s.state.NextID)
		delay := 0
		if f != nil && f.Code == "INVITE_DELAY" {
			delay = f.SettleAfterMs
		}
		g.InviteReady = now.Add(time.Duration(delay) * time.Millisecond)
		g.InviteExpired = false
		s.commitHTTP(w, r, 200, map[string]any{"inviteLink": g.Invite, "readyAfterMs": delay}, nil)
	case "join":
		pid := s.state.Accounts[aid].PlatformID
		if g.Members[pid] {
			fail(w, &APIError{409, "ALREADY_MEMBER", 0})
			return
		}
		if in.InviteLink != g.Invite || g.InviteExpired {
			fail(w, &APIError{410, "INVITE_EXPIRED", 0})
			return
		}
		if now.Before(g.InviteReady) {
			fail(w, &APIError{409, "INVITE_NOT_READY", 0})
			return
		}
		if f == nil || f.Code != "JOIN_MISSING" {
			s.state.Jobs = append(s.state.Jobs, job{Kind: "join", Due: now.Add(100 * time.Millisecond), GroupID: gid, AccountID: aid, PlatformID: pid})
		}
		s.commitHTTP(w, r, 202, map[string]bool{"accepted": true}, nil)
	case "promote":
		if in.ByAccountID != g.Owner {
			fail(w, &APIError{403, "NO_PERMISSION", 0})
			return
		}
		target := s.state.Accounts[in.AccountID]
		if target == nil || !g.Members[target.PlatformID] {
			fail(w, &APIError{409, "NOT_MEMBER_YET", 0})
			return
		}
		g.Admins[in.AccountID] = true
		s.commitHTTP(w, r, 200, map[string]any{}, nil)
	case "kick":
		owner := s.state.Accounts[g.Owner]
		if !g.Members[owner.PlatformID] {
			fail(w, &APIError{409, "OWNER_LEFT", 0})
			return
		}
		if aid != g.Owner && !g.Admins[aid] {
			fail(w, &APIError{403, "NO_PERMISSION", 0})
			return
		}
		if in.TargetID == "" {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		if f != nil && f.Code == "NETWORK_TIMEOUT" {
			if f.Effect {
				s.state.Jobs = append(s.state.Jobs, job{Kind: "kick", Due: now.Add(settleDelay(f)), GroupID: gid, PlatformID: in.TargetID})
			}
			s.commitHTTP(w, r, 0, nil, faultError(f))
			return
		}
		if g.Members[in.TargetID] {
			delete(g.Members, in.TargetID)
			s.state.Jobs = append(s.state.Jobs, job{Kind: "member_left", Due: now.Add(100 * time.Millisecond), GroupID: gid, PlatformID: in.TargetID})
		}
		s.commitHTTP(w, r, 200, map[string]bool{"kicked": true}, nil)
	case "leave":
		pid := s.state.Accounts[aid].PlatformID
		if g.Members[pid] {
			delete(g.Members, pid)
			s.state.Jobs = append(s.state.Jobs, job{Kind: "member_left", Due: now.Add(100 * time.Millisecond), GroupID: gid, PlatformID: pid})
		}
		s.commitHTTP(w, r, 200, map[string]any{}, nil)
	case "send":
		a := s.state.Accounts[aid]
		if now.Before(a.RateUntil) {
			a.RateUntil = now.Add(time.Duration(a.RetrySeconds) * time.Second)
			s.commitHTTP(w, r, 0, nil, &APIError{429, "RATE_LIMITED", a.RetrySeconds})
			return
		}
		if f != nil && f.Code == "RATE_LIMITED" {
			a.RetrySeconds = f.RetryAfterSeconds
			if a.RetrySeconds <= 0 {
				a.RetrySeconds = 1
			}
			a.RateUntil = now.Add(time.Duration(a.RetrySeconds) * time.Second)
			s.commitHTTP(w, r, 0, nil, &APIError{429, "RATE_LIMITED", a.RetrySeconds})
			return
		}
		if !g.Writable {
			fail(w, &APIError{403, "GROUP_WRITE_FORBIDDEN", 0})
			return
		}
		if !g.Members[a.PlatformID] {
			fail(w, &APIError{403, "SENDER_NOT_IN_GROUP", 0})
			return
		}
		if in.ClientID == "" || in.Text == "" {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		delay := 50 * time.Millisecond
		kind, code := "send", ""
		if f != nil {
			if f.Code == "NETWORK_TIMEOUT" {
				if !f.Effect {
					s.commitHTTP(w, r, 0, nil, faultError(f))
					return
				}
				delay = settleDelay(f)
			}
			if f.Code == "ASYNC_FAILED" {
				kind = "message_failed"
				code = "GROUP_WRITE_FORBIDDEN"
				if f.Effect {
					code = "ACCOUNT_SUSPENDED"
				}
			}
		}
		s.state.Jobs = append(s.state.Jobs, job{Kind: kind, Due: now.Add(delay), GroupID: gid, AccountID: aid, ClientID: in.ClientID, Text: in.Text, Code: code})
		if f != nil && f.Code == "NETWORK_TIMEOUT" {
			s.commitHTTP(w, r, 0, nil, faultError(f))
			return
		}
		s.commitHTTP(w, r, 202, map[string]bool{"accepted": true}, nil)
	default:
		fail(w, &APIError{404, "NOT_FOUND", 0})
	}
}
func settleDelay(f *Fault) time.Duration {
	ms := f.SettleAfterMs
	if ms <= 0 {
		ms = 100
	}
	if ms > 1900 {
		ms = 1900
	}
	return time.Duration(ms) * time.Millisecond
}
