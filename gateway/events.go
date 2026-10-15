package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (s *Service) eventsHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if f := s.consumeFault("events", "", ""); f != nil {
		s.commitHTTP(w, r, 0, nil, faultError(f))
		s.mu.Unlock()
		return
	}
	cursor := s.state.NextEvent
	raw, present := r.URL.Query()["since"]
	if present {
		var err error
		if len(raw) == 0 {
			err = fmt.Errorf("missing cursor")
		} else {
			cursor, err = strconv.ParseInt(raw[0], 10, 64)
		}
		if err != nil || cursor < 0 {
			s.mu.Unlock()
			fail(w, &APIError{400, "INVALID_CURSOR", 0})
			return
		}
	}
	s.mu.Unlock()
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, &APIError{500, "STREAM_UNSUPPORTED", 0})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	sent := 0
	for {
		s.mu.Lock()
		events := make([]Event, 0)
		for _, e := range s.state.Events {
			if e.EventID > cursor {
				events = append(events, e)
			}
		}
		d := s.state.Delivery
		s.mu.Unlock()
		if d.Reorder {
			for i := 0; i+1 < len(events); i += 2 {
				events[i], events[i+1] = events[i+1], events[i]
			}
		}
		for _, e := range events {
			b, err := json.Marshal(e.Data)
			if err != nil {
				return
			}
			copies := 1
			if d.Duplicate {
				copies = 2
			}
			for i := 0; i < copies; i++ {
				if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.EventID, e.Type, b); err != nil {
					return
				}
				sent++
				flusher.Flush()
				if d.DisconnectAfter > 0 && sent >= d.DisconnectAfter {
					return
				}
			}
			if e.EventID > cursor {
				cursor = e.EventID
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

type injectedEvent struct {
	Type    string         `json:"type"`
	Data    map[string]any `json:"data"`
	DelayMs int            `json:"delayMs"`
}

func (s *Service) adminHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.PathValue("operation") {
	case "faults":
		var f Fault
		if decode(r, &f) != nil || f.Operation == "" || f.Code == "" || f.Count < -1 || f.SettleAfterMs < 0 {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		if f.Count == 0 {
			f.Count = 1
		}
		s.state.Faults = append(s.state.Faults, f)
	case "delivery":
		var d delivery
		if decode(r, &d) != nil {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		s.state.Delivery = d
	case "events":
		var e injectedEvent
		if decode(r, &e) != nil || e.Data == nil {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		switch e.Type {
		case "message", "message_sent", "message_failed", "member_joined", "member_left", "account_status":
		default:
			fail(w, &APIError{400, "INVALID_EVENT", 0})
			return
		}
		if e.Type == "account_status" {
			aid, _ := e.Data["accountId"].(string)
			status, _ := e.Data["status"].(string)
			if (status != "suspended" && status != "session_expired" && status != "offline") || s.state.Accounts[aid] == nil {
				fail(w, &APIError{400, "INVALID_INPUT", 0})
				return
			}
			if status == "offline" {
				cid, _ := e.Data["connectionId"].(string)
				if !s.ordinaryOffline(aid, cid) {
					s.emit(e.Type, e.Data)
				}
			} else {
				s.terminal(aid, status)
			}
		} else {
			gid, _ := e.Data["groupId"].(string)
			pid, _ := e.Data["platformUserId"].(string)
			if e.Type == "member_joined" || e.Type == "member_left" {
				g := s.state.Groups[gid]
				if g == nil || pid == "" {
					fail(w, &APIError{400, "INVALID_INPUT", 0})
					return
				}
				if e.Type == "member_joined" {
					g.Members[pid] = true
				} else {
					delete(g.Members, pid)
				}
			}
			if e.DelayMs > 0 {
				s.state.Jobs = append(s.state.Jobs, job{Kind: e.Type, Due: timeNow().Add(time.Duration(e.DelayMs) * time.Millisecond), GroupID: gid, PlatformID: pid, Data: e.Data})
			} else {
				s.emit(e.Type, e.Data)
			}
		}
	case "group":
		var v struct {
			GroupID       string `json:"groupId"`
			Writable      *bool  `json:"writable"`
			InviteExpired *bool  `json:"inviteExpired"`
		}
		if decode(r, &v) != nil || s.state.Groups[v.GroupID] == nil {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		g := s.state.Groups[v.GroupID]
		if v.Writable != nil {
			g.Writable = *v.Writable
		}
		if v.InviteExpired != nil {
			g.InviteExpired = *v.InviteExpired
		}
	case "reset":
		s.state = initialState()
		s.state.Mode = s.cfg.Mode
	default:
		fail(w, &APIError{404, "NOT_FOUND", 0})
		return
	}
	s.commitHTTP(w, r, 200, map[string]bool{"ok": true}, nil)
}
