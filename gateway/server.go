package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config configures the real Telegram gateway and its durable store.
type Config struct {
	Mode     string
	Store    *PostgresStore
	Telegram *TelegramAdapter
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
	Title            string   `json:"title"`
	AccountIDs       []string `json:"accountIds"`
}

// Service provides the section 2.1 HTTP/SSE protocol.
type Service struct {
	mu     sync.Mutex
	state  state
	store  *PostgresStore
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mux    *http.ServeMux
}

// New reconstructs real account state before resuming managed connections.
func New(parent context.Context, c Config) (*Service, error) {
	if c.Store == nil {
		return nil, errors.New("gateway store required")
	}
	if c.Mode != "" && c.Mode != "telegram" {
		return nil, errors.New("GATEWAY_MODE must be telegram; simulation is no longer supported")
	}
	c.Mode = "telegram"
	if c.Telegram == nil {
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
	if len(b) > 0 && s.state.Mode != "telegram" {
		cancel()
		return nil, errors.New("gateway database has legacy or unknown provenance; preserve it and reupload credentials to a real gateway database")
	}
	s.state.Mode = c.Mode
	// Pre-generation online snapshots have no incarnation shared with the backend.
	// Persist offline before Resume; only a later explicit connect may assign an ID.
	orphaned := 0
	for id, account := range s.state.Accounts {
		if account.Status == "online" && account.ConnectionID == "" {
			account.Status = "offline"
		}
		if s.finishFailedConnection(id, account.ConnectionID) {
			orphaned++
		}
	}
	if orphaned != 0 {
		slog.Info("ended orphaned Telegram connection intents", "count", orphaned)
	}
	if err = c.Telegram.Bind(s); err != nil {
		cancel()
		return nil, err
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
	s.mux.HandleFunc("POST /accounts/register", s.registerHTTP)
	s.mux.HandleFunc("DELETE /accounts/{accountId}", s.deleteAccountHTTP)
	s.mux.HandleFunc("GET /accounts/{accountId}/registration", s.registrationHTTP)
	s.mux.HandleFunc("POST /accounts/{accountId}/check", s.inspectHTTP)
	s.mux.HandleFunc("POST /accounts/{accountId}/join", s.inspectHTTP)
	s.mux.HandleFunc("GET /accounts/{accountId}/joined-groups", s.inspectHTTP)
	s.mux.HandleFunc("POST /groups", s.groupHTTP)
	s.mux.HandleFunc("POST /groups/import", s.importHTTP)
	s.mux.HandleFunc("POST /groups/{groupId}/{operation}", s.groupHTTP)
	s.mux.HandleFunc("GET /groups/{groupId}/members", s.groupHTTP)
	s.mux.HandleFunc("GET /groups/{groupId}/recovery", s.recoveryHTTP)
	s.mux.HandleFunc("GET /groups/{groupId}/messages/by-client-id/{clientMsgId}", s.queryHTTP)
	s.mux.HandleFunc("GET /events", s.eventsHTTP)
	s.mux.HandleFunc("GET /media/{id}", func(w http.ResponseWriter, r *http.Request) { fail(w, &APIError{404, "MEDIA_EXPIRED", 0}) })
	c.Telegram.Resume(s)
	s.wg.Add(1)
	go s.runSendRecovery(c.Telegram)
	s.wg.Add(1)
	go s.watchLease()
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
	leaseCtx, leaseCancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
	stopLease := context.AfterFunc(s.ctx, leaseCancel)
	err := s.store.CheckLease(leaseCtx)
	stopLease()
	leaseCancel()
	if err != nil {
		s.cancel()
		return nil, func() {}, err
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
func (s *Service) watchLease() {
	defer s.wg.Done()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
			err := s.store.CheckLease(ctx)
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
	if err := s.store.CheckAccountRetired(opCtx, id); err != nil {
		fail(w, err)
		return
	}
	if err := s.accountError(id, false); err != nil {
		fail(w, err)
		return
	}
	a := s.state.Accounts[id]
	switch op {
	case "connect":
		var request struct {
			ConnectionID string `json:"connectionId"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if e := decoder.Decode(&request); e != nil && !errors.Is(e, io.EOF) || len(request.ConnectionID) > 128 {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		if request.ConnectionID == "" {
			request.ConnectionID = a.ConnectionID
			if request.ConnectionID == "" {
				nonce, err := randomID()
				if err != nil {
					fail(w, err)
					return
				}
				request.ConnectionID = "legacy-" + stringID(nonce)
			}
		}
		if request.ConnectionID != "" && a.EndedConnections[request.ConnectionID] {
			fail(w, &APIError{409, "CONNECTION_ENDED", 0})
			return
		}
		if a.ConnectionID != "" && a.ConnectionID != request.ConnectionID {
			fail(w, &APIError{409, "CONNECTION_CONFLICT", 0})
			return
		}
		if a.ConnectionID != request.ConnectionID {
			a.ConnectionID = request.ConnectionID
			if e := s.save(opCtx); e != nil {
				fail(w, e)
				return
			}
		}
		opCtx = context.WithValue(opCtx, connectionIDKey{}, request.ConnectionID)
		s.mu.Unlock()
		pid, e := s.cfg.Telegram.Connect(opCtx, id)
		s.mu.Lock()
		if err := opCtx.Err(); err != nil {
			if e != nil && failedConnectionStopped(e) {
				if cleanupErr := s.persistFailedConnection(id, request.ConnectionID); cleanupErr != nil {
					fail(w, cleanupErr)
					return
				}
			}
			fail(w, err)
			return
		}
		if err := s.accountError(id, false); err != nil {
			s.commitHTTP(w, r, 0, nil, err)
			return
		}
		if current := s.state.Accounts[id]; current.ConnectionID != request.ConnectionID || request.ConnectionID != "" && current.EndedConnections[request.ConnectionID] {
			fail(w, &APIError{409, "CONNECTION_ENDED", 0})
			return
		}
		if e != nil {
			failure := s.accountFailure(id, e)
			if failedConnectionStopped(e) {
				s.finishFailedConnection(id, request.ConnectionID)
			}
			s.commitHTTP(w, r, 0, nil, failure)
			return
		}
		a = s.state.Accounts[id]
		a.PlatformID = pid
		a.Status = "online"
		s.commitHTTP(w, r, 200, map[string]string{"platformUserId": a.PlatformID, "connectionId": a.ConnectionID}, nil)
	case "disconnect":
		connectionID := a.ConnectionID
		opCtx = context.WithValue(opCtx, connectionIDKey{}, connectionID)
		s.mu.Unlock()
		e := s.cfg.Telegram.Disconnect(opCtx, id)
		s.mu.Lock()
		if err := opCtx.Err(); err != nil {
			fail(w, err)
			return
		}
		if err := s.accountError(id, false); err != nil {
			s.commitHTTP(w, r, 0, nil, err)
			return
		}
		if s.state.Accounts[id].ConnectionID != connectionID {
			fail(w, &APIError{409, "CONNECTION_ENDED", 0})
			return
		}
		if e != nil {
			s.commitHTTP(w, r, 0, nil, s.accountFailure(id, e))
			return
		}
		a = s.state.Accounts[id]
		a.Status = "offline"
		s.endConnection(a)
		s.commitHTTP(w, r, 200, map[string]any{}, nil)
	default:
		fail(w, &APIError{404, "NOT_FOUND", 0})
	}
}
func (s *Service) queryHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gid, cid := r.PathValue("groupId"), r.PathValue("clientMsgId")
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
	for _, e := range s.state.Effects {
		if e.GroupID == gid && e.ClientID == cid && e.State != "sent" && e.State != "failed" {
			fail(w, &APIError{504, "NETWORK_TIMEOUT", 0})
			return
		}
	}
	fail(w, &APIError{404, "MESSAGE_NOT_FOUND", 0})
}
func timeNow() time.Time            { return time.Now().UTC().Truncate(time.Millisecond) }
func inviteHash(link string) string { parts := strings.Split(link, "+"); return parts[len(parts)-1] }
