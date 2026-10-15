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
	for {
		s.mu.Lock()
		events := make([]Event, 0)
		for _, e := range s.state.Events {
			if e.EventID > cursor {
				events = append(events, e)
			}
		}
		s.mu.Unlock()
		for _, e := range events {
			b, err := json.Marshal(e.Data)
			if err != nil {
				return
			}
			if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.EventID, e.Type, b); err != nil {
				return
			}
			flusher.Flush()
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
