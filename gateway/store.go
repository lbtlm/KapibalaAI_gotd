package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func (s *Service) save(ctx context.Context) error {
	b, e := json.Marshal(s.state)
	if e != nil {
		return e
	}
	if e = s.store.Save(ctx, b); e == nil {
		return nil
	}
	if errors.Is(e, ErrWriterLeaseLost) {
		s.cancel()
		return e
	}
	// Failed commits must not become observable through SSE or subsequent HTTP.
	recovery, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	old, loadErr := s.store.Load(recovery)
	if loadErr != nil {
		s.cancel()
		return e
	}
	if len(old) > 0 {
		var committed state
		if json.Unmarshal(old, &committed) != nil {
			s.cancel()
			return e
		}
		s.state = committed
	} else {
		s.state = initialState()
		s.state.Mode = s.cfg.Mode
	}
	return e
}
