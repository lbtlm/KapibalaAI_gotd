package gateway

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type onlineSession struct {
	intentionalStop bool
	ready           bool
	attemptContext  context.Context
	connectionID    string
	client          *telegram.Client
	api             *tg.Client
	cancel          context.CancelFunc
	done            chan struct{}
	self            *tg.User
}

// TelegramAdapter owns one gotd Run/update manager per configured account.
type TelegramAdapter struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	db         *sql.DB
	configs    map[string]AccountConfig
	sessions   map[string]*onlineSession
	service    *Service
	recovering map[string]bool
}

// NewTelegramAdapter validates configuration without opening or connecting sessions.
func NewTelegramAdapter(ctx context.Context, db *sql.DB) (*TelegramAdapter, error) {
	lifetime, cancel := context.WithCancel(ctx)
	a := &TelegramAdapter{ctx: lifetime, cancel: cancel, db: db, configs: map[string]AccountConfig{}, sessions: map[string]*onlineSession{}}
	if db == nil {
		cancel()
		return nil, errors.New("Telegram database required")
	}
	rows, err := db.QueryContext(ctx, "SELECT account_id,config FROM gateway_account_configs")
	if err != nil {
		cancel()
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var b []byte
		var c AccountConfig
		if err = rows.Scan(&id, &b); err == nil {
			err = json.Unmarshal(b, &c)
		}
		if err != nil || c.ID != id || !validDurableConfig(c) {
			cancel()
			return nil, errors.New("invalid durable Telegram configuration; restore credentials without overwriting existing data")
		}
		a.configs[id] = c
	}
	if err = rows.Err(); err != nil {
		cancel()
		return nil, err
	}
	return a, nil
}

// Bind keeps persisted accounts intact and refuses missing durable credentials.
func (a *TelegramAdapter) Bind(s *Service) error {
	for id := range s.state.Accounts {
		if _, ok := a.configs[id]; !ok {
			return errors.New("account lacks durable Telegram configuration; run offline adopt or reupload credentials")
		}
	}
	a.service = s
	go func() { <-s.ctx.Done(); a.cancel() }()
	for id := range a.configs {
		if s.state.Accounts[id] == nil {
			s.state.Accounts[id] = &account{Status: "idle"}
		}
	}
	return nil
}

// Resume reconnects persisted online aliases and resumes their existing send intents.
func (a *TelegramAdapter) Resume(s *Service) {
	for id, acct := range s.state.Accounts {
		if acct.Status != "online" {
			continue
		}
		id := id
		connectionID := acct.ConnectionID
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			attemptCtx, finish := context.WithCancel(s.ctx)
			defer finish()
			started := time.Now()
			pid, err := a.Connect(context.WithValue(attemptCtx, connectionIDKey{}, connectionID), id)
			s.mu.Lock()
			acct := s.state.Accounts[id]
			if acct == nil || acct.Status != "online" || acct.ConnectionID != connectionID || s.ctx.Err() != nil {
				s.mu.Unlock()
				return
			}
			if err == nil {
				acct.PlatformID = pid
			} else {
				e := platformError(err)
				slog.Warn("Telegram resume failed", "code", e.Code, "elapsedMs", time.Since(started).Milliseconds())
				switch e.Code {
				case "ACCOUNT_SUSPENDED":
					s.terminal(id, "suspended")
				case "SESSION_EXPIRED":
					s.terminal(id, "session_expired")
				default:
					s.ordinaryOffline(id, connectionID)
				}
			}
			if e := s.save(context.Background()); e != nil {
				s.cancel()
			}
			s.mu.Unlock()
			if err == nil {
				_ = a.RecoverEffects(s.ctx, id)
			}
		}()
	}
}

// Connect adapts the source online_manager_lifecycle.go client.Run/Self/updates loop.
func (a *TelegramAdapter) Connect(ctx context.Context, id string) (pid string, err error) {
	a.mu.Lock()
	locked := true
	defer func() {
		if locked {
			a.mu.Unlock()
		}
	}()
	attempted := a.sessions[id]
	ownsAttempt := false
	defer func() {
		if err != nil && attempted != nil {
			var done <-chan struct{}
			if ownsAttempt {
				done = attempted.done
			}
			err = &connectionAttemptError{err: err, done: done}
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if a.ctx != nil && a.ctx.Err() != nil {
		return "", a.ctx.Err()
	}
	if s := a.sessions[id]; s != nil {
		ended := false
		select {
		case <-s.done:
			ended = true
		default:
		}
		cid, _ := ctx.Value(connectionIDKey{}).(string)
		if ended {
			if cid == "" || cid == s.connectionID {
				return "", &APIError{409, "CONNECTION_ENDED", 0}
			}
			delete(a.sessions, id)
			attempted = nil
		} else if cid != "" && s.connectionID != cid {
			return "", &APIError{409, "CONNECTION_CONFLICT", 0}
		} else {
			if !s.ready {
				return "", &APIError{409, "ACCOUNT_IN_USE", 0}
			}
			return stringID(s.self.ID), nil
		}
	}
	c, ok := a.configs[id]
	if !ok {
		return "", &APIError{404, "ACCOUNT_NOT_FOUND", 0}
	}
	runCtx, cancel := context.WithCancel(a.ctx)
	cid, _ := ctx.Value(connectionIDKey{}).(string)
	s := &onlineSession{connectionID: cid, attemptContext: ctx, cancel: cancel, done: make(chan struct{})}
	attempted, ownsAttempt = s, true
	// Reserve this account before storage/network work. Other accounts never
	// wait for its startup; duplicate callers cannot start another client.
	a.sessions[id] = s
	a.mu.Unlock()
	locked = false
	runStarted := false
	defer func() {
		if err != nil {
			cancel()
		}
		if !runStarted {
			close(s.done)
		}
	}()
	connectCtx, connectCancel := context.WithTimeout(ctx, 30*time.Second)
	stopRun := context.AfterFunc(runCtx, connectCancel)
	defer func() { stopRun(); connectCancel() }()
	storage := dbSession{a.db, id}
	if _, err := storage.LoadSession(connectCtx); err != nil {
		if connectCtx.Err() != nil {
			return "", connectCtx.Err()
		}
		return "", &APIError{503, "CREDENTIALS_INCOMPLETE", 0}
	}
	var manager *updates.Manager
	client := telegram.NewClient(c.APIID, c.APIHash, telegram.Options{SessionStorage: storage, UpdateHandler: telegram.UpdateHandlerFunc(func(ctx context.Context, u tg.UpdatesClass) error { return manager.Handle(ctx, u) }), MigrationTimeout: 120 * time.Second, Device: telegram.DeviceConfig{DeviceModel: c.DeviceModel, SystemVersion: c.SystemVersion, AppVersion: c.AppVersion, LangCode: "en", SystemLangCode: "en-US"}})
	s.client, s.api = client, a.fencedClient(tg.NewClient(client))
	pipeline := a.Updates(id, s.api, cancel)
	manager = updates.New(updates.Config{Handler: pipeline.Handler, Storage: pipeline.Storage, AccessHasher: pipeline.AccessHasher, OnChannelTooLong: pipeline.OnChannelTooLong})
	ready := make(chan error, 1)
	runStarted = true
	go func() {
		defer close(s.done)
		err := client.Run(runCtx, func(live context.Context) error {
			self, e := client.Self(live)
			if e != nil {
				return e
			}
			s.self = self
			return pipeline.Run(live, manager, self.ID, updates.AuthOptions{IsBot: self.Bot, OnStart: func(context.Context) { ready <- nil }})
		})
		select {
		case ready <- err:
		default:
		}
		if failure := pipeline.gate.err(); failure != nil {
			err = failure
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("Telegram connection ended", "accountId", id, "code", platformError(err).Error())
		}
		if a.service != nil {
			go func() {
				<-s.done
				a.connectionEnded(id, s, err)
			}()
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			cancel()
			return "", platformError(err)
		}
		if err := connectCtx.Err(); err != nil {
			cancel()
			return "", platformError(err)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.sessions[id] != s || s.intentionalStop || runCtx.Err() != nil || s.self == nil {
			return "", &APIError{409, "CONNECTION_ENDED", 0}
		}
		s.ready = true
		return stringID(s.self.ID), nil
	case <-connectCtx.Done():
		cancel()
		return "", &APIError{504, "NETWORK_TIMEOUT", 0}
	}
}
func (a *TelegramAdapter) Disconnect(ctx context.Context, id string) error {
	a.mu.Lock()
	if err := ctx.Err(); err != nil {
		a.mu.Unlock()
		return err
	}
	s := a.sessions[id]
	if s != nil {
		if cid, _ := ctx.Value(connectionIDKey{}).(string); cid != "" && s.connectionID != "" && s.connectionID != cid {
			a.mu.Unlock()
			return &APIError{409, "CONNECTION_ENDED", 0}
		}
		s.intentionalStop = true
		s.cancel()
	}
	a.mu.Unlock()
	if s != nil {
		select {
		case <-s.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (a *TelegramAdapter) Close() error {
	a.cancel()
	a.mu.Lock()
	sessions := a.sessions
	a.sessions = map[string]*onlineSession{}
	a.mu.Unlock()
	for _, s := range sessions {
		s.cancel()
		<-s.done
	}
	return nil
}
func (a *TelegramAdapter) online(id string) (*onlineSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[id]
	if s == nil || !s.ready {
		return nil, &APIError{409, "ACCOUNT_OFFLINE", 0}
	}
	select {
	case <-s.done:
		return nil, &APIError{409, "ACCOUNT_OFFLINE", 0}
	default:
		return s, nil
	}
}
func (a *TelegramAdapter) connectionEnded(id string, old *onlineSession, err error) {
	a.mu.Lock()
	if a.sessions[id] != old || old.intentionalStop {
		a.mu.Unlock()
		return
	}
	if !old.ready {
		a.mu.Unlock()
		// The HTTP/Resume result must finish classification and persistence before
		// cleanup can end its CID. Otherwise cleanup masks the original error.
		context.AfterFunc(old.attemptContext, func() {
			a.mu.Lock()
			current := a.sessions[id] == old && !old.intentionalStop
			a.mu.Unlock()
			if !current {
				return
			}
			s := a.service
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.persistFailedConnection(id, old.connectionID); err != nil {
				s.cancel()
			}
		})
		return
	}
	a.mu.Unlock()
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	acct := s.state.Accounts[id]
	if acct == nil || acct.ConnectionID != old.connectionID || acct.Status == "suspended" || acct.Status == "session_expired" {
		return
	}
	e := platformError(err)
	if e == nil {
		e = &APIError{409, "ACCOUNT_OFFLINE", 0}
	}
	switch e.Code {
	case "ACCOUNT_SUSPENDED":
		s.terminal(id, "suspended")
	case "SESSION_EXPIRED":
		s.terminal(id, "session_expired")
	default:
		s.ordinaryOffline(id, old.connectionID)
	}
	if err = s.save(context.Background()); err != nil {
		slog.Error("persist Telegram disconnection", "accountId", id, "error", err)
		s.cancel()
	}
}
func platformError(err error) *APIError {
	if err == nil {
		return nil
	}
	var api *APIError
	if errors.As(err, &api) {
		return api
	}
	if wait, ok := tgerr.AsFloodWait(err); ok {
		return &APIError{429, "RATE_LIMITED", int((wait + time.Second - 1) / time.Second)}
	}
	code := strings.ToUpper(err.Error())
	permanentCode := code
	if rpc, ok := tgerr.As(err); ok {
		permanentCode = rpc.Type
	}
	switch permanentCode {
	case "USERNAME_OCCUPIED", "USERNAME_INVALID", "USERNAME_PURCHASE_AVAILABLE", "CHANNELS_ADMIN_PUBLIC_TOO_MUCH":
		return &APIError{422, permanentCode, 0}
	case "USER_DEACTIVATED", "USER_DEACTIVATED_BAN", "PHONE_NUMBER_BANNED", "USER_RESTRICTED":
		return &APIError{403, "ACCOUNT_SUSPENDED", 0}
	case "AUTH_KEY_UNREGISTERED", "AUTH_KEY_INVALID", "SESSION_REVOKED", "SESSION_EXPIRED":
		return &APIError{401, "SESSION_EXPIRED", 0}
	}
	if strings.Contains(code, "INVITE_HASH_EXPIRED") || strings.Contains(code, "INVITE_HASH_INVALID") {
		return &APIError{410, "INVITE_EXPIRED", 0}
	}
	if strings.Contains(code, "USER_ALREADY_PARTICIPANT") {
		return &APIError{409, "ALREADY_MEMBER", 0}
	}
	if strings.Contains(code, "USER_NOT_PARTICIPANT") {
		return &APIError{403, "SENDER_NOT_IN_GROUP", 0}
	}
	if strings.Contains(code, "CHAT_WRITE_FORBIDDEN") || strings.Contains(code, "CHANNEL_PRIVATE") {
		return &APIError{403, "GROUP_WRITE_FORBIDDEN", 0}
	}
	if strings.Contains(code, "CHAT_ADMIN_REQUIRED") {
		return &APIError{403, "NO_PERMISSION", 0}
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(code, "TIMEOUT") {
		return &APIError{504, "NETWORK_TIMEOUT", 0}
	}
	return &APIError{503, "GATEWAY_UNAVAILABLE", 0}
}
func randomID() (int64, error) {
	var b [8]byte
	if _, e := rand.Read(b[:]); e != nil {
		return 0, e
	}
	v := int64(binary.LittleEndian.Uint64(b[:]))
	if v == 0 {
		return randomID()
	}
	return v, nil
}
