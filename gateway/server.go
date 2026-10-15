package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config selects a transport and an explicitly enabled fake control API.
type Config struct {
	Mode         string
	AdminEnabled bool
	Store        Store
	Telegram     Adapter
}

// Adapter translates real platform operations; fake operations run in the same handler.
type Adapter interface {
	Connect(context.Context, string) (string, error)
	Disconnect(context.Context, string) error
	Execute(context.Context, string, string, input, *Service) (any, error)
	Close() error
}

// APIError preserves only the stable public gateway error.
type APIError struct {
	Status            int
	Code              string
	RetryAfterSeconds int
}

func (e *APIError) Error() string { return e.Code }

type input struct {
	AccountID        string   `json:"accountId"`
	CreatorAccountID string   `json:"creatorAccountId"`
	ByAccountID      string   `json:"byAccountId"`
	TargetID         string   `json:"targetPlatformUserId"`
	InviteLink       string   `json:"inviteLink"`
	ClientID         string   `json:"clientMsgId"`
	Text             string   `json:"text"`
	IdempotencyKey   string   `json:"idempotencyKey"`
	Username         string   `json:"username"`
	AccountIDs       []string `json:"accountIds"`
}

// Service provides the section 2.1 HTTP/SSE protocol.
type Service struct {
	mu     sync.Mutex
	state  state
	store  Store
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mux    *http.ServeMux
}

// New reconstructs state before starting durable delayed fake effects.
func New(parent context.Context, c Config) (*Service, error) {
	if c.Store == nil {
		return nil, errors.New("gateway store required")
	}
	if c.Mode != "fake" && c.Mode != "telegram" {
		return nil, errors.New("GATEWAY_MODE must be fake or telegram")
	}
	if c.Mode == "telegram" && c.Telegram == nil {
		return nil, errors.New("telegram adapter required")
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Service{state: initialState(), store: c.Store, cfg: c, ctx: ctx, cancel: cancel, mux: http.NewServeMux()}
	b, err := c.Store.Load(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	if len(b) > 0 {
		if err = json.Unmarshal(b, &s.state); err != nil {
			cancel()
			return nil, err
		}
	}
	if s.state.Mode != "" && s.state.Mode != c.Mode {
		cancel()
		return nil, errors.New("gateway database mode differs from GATEWAY_MODE; use separate databases")
	}
	s.state.Mode = c.Mode
	if binder, ok := c.Telegram.(interface{ Bind(*Service) }); ok {
		binder.Bind(s)
	}
	if s.state.CreateKeys == nil {
		s.state.CreateKeys = map[string]string{}
	}
	if err = s.save(ctx); err != nil {
		cancel()
		return nil, err
	}
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]string{"status": "ok", "mode": c.Mode})
	})
	s.mux.HandleFunc("POST /accounts/{accountId}/{operation}", s.accountHTTP)
	s.mux.HandleFunc("POST /groups", s.groupHTTP)
	s.mux.HandleFunc("POST /groups/import", s.importHTTP)
	s.mux.HandleFunc("POST /groups/{groupId}/{operation}", s.groupHTTP)
	s.mux.HandleFunc("GET /groups/{groupId}/members", s.groupHTTP)
	s.mux.HandleFunc("GET /groups/{groupId}/recovery", s.recoveryHTTP)
	s.mux.HandleFunc("GET /groups/{groupId}/messages/by-client-id/{clientMsgId}", s.queryHTTP)
	s.mux.HandleFunc("GET /events", s.eventsHTTP)
	s.mux.HandleFunc("GET /media/{id}", func(w http.ResponseWriter, r *http.Request) { fail(w, &APIError{404, "MEDIA_EXPIRED", 0}) })
	if c.Mode == "fake" && c.AdminEnabled {
		s.mux.HandleFunc("POST /admin/{operation}", s.adminHTTP)
	}
	s.wg.Add(1)
	go s.runJobs()
	if a, ok := c.Telegram.(interface{ Resume(*Service) }); ok {
		a.Resume(s)
	}
	if a, ok := c.Telegram.(interface {
		RecoverEffects(context.Context, string) error
	}); ok {
		s.wg.Add(1)
		go s.runSendRecovery(a)
	}
	if checker, ok := c.Store.(interface{ CheckLease(context.Context) error }); ok {
		s.wg.Add(1)
		go s.watchLease(checker)
	}
	return s, nil
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, stop, err := s.operationContext(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	defer stop()
	s.mux.ServeHTTP(w, r.WithContext(ctx))
}

// Done closes on shutdown or a lost PostgreSQL writer lease.
func (s *Service) Done() <-chan struct{} { return s.ctx.Done() }

// operationContext checks ownership after queueing and binds an RPC to service life.
func (s *Service) operationContext(parent context.Context) (context.Context, func(), error) {
	if err := parent.Err(); err != nil {
		return nil, func() {}, err
	}
	if s.ctx.Err() != nil {
		return nil, func() {}, ErrWriterLeaseLost
	}
	if checker, ok := s.store.(interface{ CheckLease(context.Context) error }); ok {
		leaseCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
		stopLease := context.AfterFunc(s.ctx, cancel)
		err := checker.CheckLease(leaseCtx)
		stopLease()
		cancel()
		if err != nil {
			s.cancel()
			return nil, func() {}, err
		}
	}
	if err := parent.Err(); err != nil {
		return nil, func() {}, err
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(s.ctx, cancel)
	if s.ctx.Err() != nil {
		cancel()
		stop()
		return nil, func() {}, ErrWriterLeaseLost
	}
	return ctx, func() { stop(); cancel() }, nil
}
func (s *Service) watchLease(checker interface{ CheckLease(context.Context) error }) {
	defer s.wg.Done()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
			err := checker.CheckLease(ctx)
			cancel()
			if err != nil {
				s.cancel()
				return
			}
		}
	}
}

// Close stops workers before releasing the durable-store writer lease.
func (s *Service) Close() {
	s.cancel()
	if s.cfg.Telegram != nil {
		_ = s.cfg.Telegram.Close()
	}
	s.wg.Wait()
	_ = s.store.Close()
}
func stringID(i int64) string { return strconv.FormatInt(i, 10) }
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	var e *APIError
	if !errors.As(err, &e) {
		slog.Error("gateway operation failed", "error", err)
		e = &APIError{503, "GATEWAY_UNAVAILABLE", 0}
	}
	v := map[string]any{"code": e.Code}
	if e.RetryAfterSeconds > 0 {
		v["retryAfterSeconds"] = e.RetryAfterSeconds
		w.Header().Set("Retry-After", strconv.Itoa(e.RetryAfterSeconds))
	}
	respond(w, e.Status, v)
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func (s *Service) accountError(id string, online bool) error {
	a := s.state.Accounts[id]
	if a == nil {
		return &APIError{404, "ACCOUNT_NOT_FOUND", 0}
	}
	if a.Status == "suspended" {
		return &APIError{403, "ACCOUNT_SUSPENDED", 0}
	}
	if a.Status == "session_expired" {
		return &APIError{401, "SESSION_EXPIRED", 0}
	}
	if online && a.Status != "online" {
		return &APIError{409, "ACCOUNT_OFFLINE", 0}
	}
	return nil
}
func (s *Service) consumeFault(op, accountID, groupID string) *Fault {
	for i := range s.state.Faults {
		f := &s.state.Faults[i]
		if f.Count == 0 || (f.Operation != "any" && f.Operation != op) || (f.AccountID != "" && f.AccountID != accountID) || (f.GroupID != "" && f.GroupID != groupID) {
			continue
		}
		v := *f
		if f.Count > 0 {
			f.Count--
		}
		return &v
	}
	return nil
}
func faultError(f *Fault) error {
	if f == nil {
		return nil
	}
	codes := map[string]int{"NETWORK_TIMEOUT": 504, "GATEWAY_UNAVAILABLE": 503, "SERVICE_UNAVAILABLE": 503, "RATE_LIMITED": 429, "ACCOUNT_SUSPENDED": 403, "SESSION_EXPIRED": 401, "GROUP_WRITE_FORBIDDEN": 403, "SENDER_NOT_IN_GROUP": 403, "INVITE_NOT_READY": 409, "INVITE_EXPIRED": 410, "ALREADY_MEMBER": 409, "NOT_MEMBER_YET": 409, "OWNER_LEFT": 409, "NO_PERMISSION": 403, "LEAVE_FAILED": 500}
	status := codes[f.Code]
	if status == 0 {
		status = 503
	}
	return &APIError{status, f.Code, f.RetryAfterSeconds}
}
func (s *Service) commitHTTP(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if e := s.save(r.Context()); e != nil {
		fail(w, e)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, status, v)
}
func (s *Service) accountHTTP(w http.ResponseWriter, r *http.Request) {
	id, op := r.PathValue("accountId"), r.PathValue("operation")
	s.mu.Lock()
	defer s.mu.Unlock()
	opCtx, stop, err := s.operationContext(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	defer stop()
	if err := s.accountError(id, false); err != nil {
		fail(w, err)
		return
	}
	if f := s.consumeFault(op, id, ""); f != nil {
		if f.Code == "ACCOUNT_SUSPENDED" {
			s.terminal(id, "suspended")
		}
		if f.Code == "SESSION_EXPIRED" {
			s.terminal(id, "session_expired")
		}
		s.commitHTTP(w, r, 0, nil, faultError(f))
		return
	}
	a := s.state.Accounts[id]
	switch op {
	case "connect":
		if s.cfg.Telegram != nil {
			s.mu.Unlock()
			pid, e := s.cfg.Telegram.Connect(opCtx, id)
			s.mu.Lock()
			if err := opCtx.Err(); err != nil {
				fail(w, err)
				return
			}
			if e != nil {
				s.commitHTTP(w, r, 0, nil, s.accountFailure(id, e))
				return
			}
			if err := s.accountError(id, false); err != nil {
				s.commitHTTP(w, r, 0, nil, err)
				return
			}
			a = s.state.Accounts[id]
			a.PlatformID = pid
		}
		a.Status = "online"
		s.commitHTTP(w, r, 200, map[string]string{"platformUserId": a.PlatformID}, nil)
	case "disconnect":
		if s.cfg.Telegram != nil {
			s.mu.Unlock()
			e := s.cfg.Telegram.Disconnect(opCtx, id)
			s.mu.Lock()
			if err := opCtx.Err(); err != nil {
				fail(w, err)
				return
			}
			if e != nil {
				s.commitHTTP(w, r, 0, nil, s.accountFailure(id, e))
				return
			}
			if err := s.accountError(id, false); err != nil {
				s.commitHTTP(w, r, 0, nil, err)
				return
			}
			a = s.state.Accounts[id]
		}
		a.Status = "offline"
		s.commitHTTP(w, r, 200, map[string]any{}, nil)
	default:
		fail(w, &APIError{404, "NOT_FOUND", 0})
	}
}
func (s *Service) queryHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gid, cid := r.PathValue("groupId"), r.PathValue("clientMsgId")
	if f := s.consumeFault("query", "", gid); f != nil {
		s.commitHTTP(w, r, 0, nil, faultError(f))
		return
	}
	var earliest *message
	for i := range s.state.Messages {
		m := &s.state.Messages[i]
		if m.GroupID != gid || m.ClientID != cid {
			continue
		}
		if earliest == nil || m.SentAt.Before(earliest.SentAt) {
			earliest = m
			continue
		}
		// Telegram dates have second precision; channel message IDs preserve the
		// platform order when recovery persists an older result after a newer one.
		if m.SentAt.Equal(earliest.SentAt) {
			id, err := strconv.ParseInt(m.MsgID, 10, 64)
			previous, previousErr := strconv.ParseInt(earliest.MsgID, 10, 64)
			if err == nil && previousErr == nil && id < previous {
				earliest = m
			}
		}
	}
	if earliest != nil {
		respond(w, 200, map[string]any{"msgId": earliest.MsgID, "sentAt": earliest.SentAt})
		return
	}
	if s.cfg.Mode == "telegram" {
		for _, e := range s.state.Effects {
			if e.GroupID == gid && e.ClientID == cid && e.State != "sent" && e.State != "failed" {
				fail(w, &APIError{504, "NETWORK_TIMEOUT", 0})
				return
			}
		}
	}
	fail(w, &APIError{404, "MESSAGE_NOT_FOUND", 0})
}
func timeNow() time.Time            { return time.Now().UTC().Truncate(time.Millisecond) }
func inviteHash(link string) string { parts := strings.Split(link, "+"); return parts[len(parts)-1] }
