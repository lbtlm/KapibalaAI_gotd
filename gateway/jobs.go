package gateway

import (
	"context"
	"log/slog"
	"time"
)

func (s *Service) emit(kind string, data map[string]any) {
	s.state.NextEvent++
	data["eventId"] = s.state.NextEvent
	data["type"] = kind
	s.state.Events = append(s.state.Events, Event{s.state.NextEvent, kind, data})
}
func (s *Service) terminal(aid, status string) {
	a := s.state.Accounts[aid]
	if a == nil {
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
	case "SESSION_EXPIRED":
		status = "session_expired"
	}
	if acct := s.state.Accounts[aid]; status != "" && acct != nil && acct.Status != status {
		s.terminal(aid, status)
	}
	return api
}
func (s *Service) runJobs() {
	defer s.wg.Done()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processJobs()
		}
	}
}
func (s *Service) processJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := timeNow()
	changed := false
	remaining := make([]job, 0, len(s.state.Jobs))
	for _, j := range s.state.Jobs {
		if now.Before(j.Due) {
			remaining = append(remaining, j)
			continue
		}
		changed = true
		g := s.state.Groups[j.GroupID]
		switch j.Kind {
		case "join":
			if g != nil {
				a := s.state.Accounts[j.AccountID]
				if a != nil && a.Status == "online" && !g.Members[j.PlatformID] {
					g.Members[j.PlatformID] = true
					remaining = append(remaining, job{Kind: "member_joined", Due: now.Add(100 * time.Millisecond), GroupID: j.GroupID, PlatformID: j.PlatformID})
				}
			}
		case "kick":
			if g != nil && g.Members[j.PlatformID] {
				delete(g.Members, j.PlatformID)
				remaining = append(remaining, job{Kind: "member_left", Due: now.Add(50 * time.Millisecond), GroupID: j.GroupID, PlatformID: j.PlatformID})
			}
			if e := s.state.Effects[j.EffectID]; e != nil {
				e.State = "completed"
			}
		case "member_joined", "member_left":
			s.emit(j.Kind, map[string]any{"groupId": j.GroupID, "platformUserId": j.PlatformID})
		case "send":
			a := s.state.Accounts[j.AccountID]
			if a == nil {
				continue
			}
			s.state.NextID++
			m := message{j.GroupID, j.ClientID, "msg-" + stringID(s.state.NextID), a.PlatformID, j.Text, now}
			s.state.Messages = append(s.state.Messages, m)
			s.emit("message_sent", map[string]any{"groupId": j.GroupID, "clientMsgId": j.ClientID, "msgId": m.MsgID, "sentAt": m.SentAt})
			s.emit("message", map[string]any{"groupId": j.GroupID, "msgId": m.MsgID, "senderPlatformUserId": m.Sender, "text": m.Text, "sentAt": m.SentAt})
		case "message_failed":
			s.emit(j.Kind, map[string]any{"groupId": j.GroupID, "clientMsgId": j.ClientID, "code": j.Code})
			if j.Code == "ACCOUNT_SUSPENDED" {
				s.terminal(j.AccountID, "suspended")
			}
		default:
			if j.Data != nil {
				s.emit(j.Kind, j.Data)
			}
		}
	}
	if changed {
		s.state.Jobs = remaining
		if err := s.save(context.Background()); err != nil {
			slog.Error("persist delayed gateway effect", "error", err)
		}
	}
}
