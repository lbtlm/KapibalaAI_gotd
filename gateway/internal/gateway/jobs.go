package gateway

func (s *Service) emit(kind string, data map[string]any) {
	s.state.NextEvent++
	data["eventId"] = s.state.NextEvent
	data["type"] = kind
	s.state.Events = append(s.state.Events, Event{s.state.NextEvent, kind, data})
}
func (s *Service) terminal(aid, status string) {
	a := s.state.Accounts[aid]
	if a == nil || a.Status == "suspended" || a.Status == "session_expired" {
		return
	}
	a.Status = status
	s.endConnection(a)
	s.emit("account_status", map[string]any{"accountId": aid, "status": status})
	for gid, g := range s.state.Groups {
		if g.Members[a.PlatformID] {
			delete(g.Members, a.PlatformID)
			s.emit("member_left", map[string]any{"groupId": gid, "platformUserId": a.PlatformID})
		}
	}
}

// accountFailure applies only explicit permanent platform failures at result boundaries.
func (s *Service) accountFailure(aid string, err error) error {
	if err == nil {
		return nil
	}
	api := platformError(err)
	status := ""
	switch api.Code {
	case "ACCOUNT_SUSPENDED":
		status = "suspended"
	case "SESSION_EXPIRED", "AUTH_KEY_DUPLICATED":
		status = "session_expired"
	}
	if acct := s.state.Accounts[aid]; status != "" && acct != nil && acct.Status != status {
		s.terminal(aid, status)
	}
	return api
}
