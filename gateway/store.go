package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Store is durable state storage. Production uses PostgreSQL; memory is a test seam.
type Store interface {
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
	Close() error
}

// MemoryStore is an isolated, restartable test store, never used by the entrypoint.
type MemoryStore struct {
	mu   sync.Mutex
	data []byte
}

// NewMemoryStore creates isolated protocol-test storage.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }
func (m *MemoryStore) Load(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.data...), nil
}
func (m *MemoryStore) Save(_ context.Context, b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = append([]byte(nil), b...)
	return nil
}
func (*MemoryStore) Close() error { return nil }
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
