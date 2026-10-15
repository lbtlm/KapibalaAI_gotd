package gateway

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type registerInput struct {
	AccountID     string `json:"accountId"`
	Session       []byte `json:"session"`
	APIID         int    `json:"apiId"`
	APIHash       string `json:"apiHash"`
	DeviceModel   string `json:"deviceModel,omitempty"`
	SystemVersion string `json:"systemVersion,omitempty"`
	AppVersion    string `json:"appVersion,omitempty"`
}

type registration struct {
	Authorization string `json:"authorization"`
	Configuration string `json:"configuration"`
}

type connectionIDKey struct{}

// Only the caller that started Run carries its completion signal. A duplicate
// caller carries nil: it must not finalize the owner's unclassified result.
type connectionAttemptError struct {
	err  error
	done <-chan struct{}
}

func (e *connectionAttemptError) Error() string { return e.err.Error() }
func (e *connectionAttemptError) Unwrap() error { return e.err }

func failedConnectionStopped(err error) bool {
	var attempt *connectionAttemptError
	if !errors.As(err, &attempt) {
		return true // Connect returned before any Run existed.
	}
	select {
	case <-attempt.done:
		return true
	default:
		return false
	}
}

// finishFailedConnection preserves A1's idle/offline status while reconciling
// the shared intent. Call only after the attempt is known to have stopped.
func (s *Service) finishFailedConnection(id, cid string) bool {
	a := s.state.Accounts[id]
	if a == nil || cid == "" || a.ConnectionID != cid || a.Status != "idle" && a.Status != "offline" {
		return false
	}
	s.emit("account_status", map[string]any{"accountId": id, "status": "offline", "connectionId": cid})
	s.endConnection(a)
	return true
}

func (s *Service) persistFailedConnection(id, cid string) error {
	if s.ctx.Err() != nil || !s.finishFailedConnection(id, cid) {
		return nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	return s.save(ctx)
}

func (s *Service) endConnection(a *account) {
	if a.ConnectionID != "" {
		if a.EndedConnections == nil {
			a.EndedConnections = map[string]bool{}
		}
		a.EndedConnections[a.ConnectionID] = true
	}
	a.ConnectionID = ""
}

func (s *Service) ordinaryOffline(id, cid string) bool {
	a := s.state.Accounts[id]
	if a == nil || a.ConnectionID != cid || a.Status == "suspended" || a.Status == "session_expired" || cid != "" && a.EndedConnections[cid] {
		return false
	}
	if cid == "" && a.Status != "online" {
		return false
	}
	a.Status = "offline"
	s.emit("account_status", map[string]any{"accountId": id, "status": "offline", "connectionId": cid})
	s.endConnection(a)
	return true
}

// AccountProfile contains only public self identity, never authorization material.
type AccountProfile struct {
	Source         string  `json:"source"`
	PlatformUserID string  `json:"platformUserId"`
	Phone          *string `json:"phone"`
	Username       *string `json:"username"`
	FirstName      *string `json:"firstName"`
	LastName       *string `json:"lastName"`
	Premium        bool    `json:"premium"`
}

type JoinedGroup struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Username *string `json:"username"`
	Type     string  `json:"type"`
	Role     string  `json:"role"`
}

func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func (s *Service) registerHTTP(w http.ResponseWriter, r *http.Request) {
	var in registerInput
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 24<<20))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || in.AccountID == "" || len(in.AccountID) > 128 || strings.ContainsAny(in.AccountID, "/\\\x00") || in.APIID <= 0 || in.APIID > 2147483647 || len(in.APIHash) != 32 || len(in.Session) > 16<<20 || !bytes.HasPrefix(in.Session, []byte("SQLite format 3\x00")) {
		fail(w, &APIError{400, "INVALID_INPUT", 0})
		return
	}
	if _, err := hex.DecodeString(in.APIHash); err != nil {
		fail(w, &APIError{400, "INVALID_INPUT", 0})
		return
	}
	// The existing converter owns SQLite validation. This private temporary file is
	// removed before registration; restart needs only the converted database row.
	f, err := os.CreateTemp("", "gateway-import-*.session")
	if err != nil {
		fail(w, err)
		return
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err = f.Write(in.Session); err != nil {
		f.Close()
		fail(w, err)
		return
	}
	if err = f.Close(); err != nil {
		fail(w, err)
		return
	}
	converted, err := ImportSession(path, "TelethonSQLite", "")
	if err != nil {
		fail(w, &APIError{400, "INVALID_SESSION", 0})
		return
	}
	authorization, err := authorizationFingerprint(r.Context(), converted)
	if err != nil {
		fail(w, &APIError{400, "INVALID_SESSION", 0})
		return
	}
	c := AccountConfig{ID: in.AccountID, APIID: in.APIID, APIHash: strings.ToLower(in.APIHash), DeviceModel: in.DeviceModel, SystemVersion: in.SystemVersion, AppVersion: in.AppVersion}
	identity := registration{authorization, configFingerprint(c)}
	s.mu.Lock()
	defer s.mu.Unlock()
	opCtx, stop, err := s.operationContext(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	defer stop()
	s.mu.Unlock()
	err = s.cfg.Telegram.Register(opCtx, c, converted, identity)
	s.mu.Lock()
	if err != nil {
		fail(w, err)
		return
	}
	commitCtx, finish, err := s.operationContext(opCtx)
	if err != nil {
		fail(w, err)
		return
	}
	defer finish()
	if err = s.store.CheckAccountRetired(commitCtx, in.AccountID); err != nil {
		fail(w, err)
		return
	}
	if s.state.Accounts[in.AccountID] == nil {
		s.state.Accounts[in.AccountID] = &account{Status: "idle"}
	}
	s.commitHTTP(w, r.WithContext(commitCtx), 200, map[string]string{"accountId": in.AccountID, "status": "idle"}, nil)
}

func (s *Service) inspectHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("accountId")
	op := "check"
	var link string
	if strings.HasSuffix(r.URL.Path, "/join") {
		var in struct {
			Link string `json:"link"`
		}
		if decode(r, &in) != nil {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
		link, op = in.Link, "join"
	}
	if r.Method == "GET" {
		op = "joined-groups"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	opCtx, stop, err := s.operationContext(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	defer stop()
	if err = s.accountError(id, true); err != nil {
		fail(w, err)
		return
	}
	connectionID := s.state.Accounts[id].ConnectionID
	s.mu.Unlock()
	var out any
	if op == "join" {
		out, err = s.cfg.Telegram.JoinLink(opCtx, id, link)
	} else {
		out, err = s.cfg.Telegram.Inspect(opCtx, id, op)
	}
	s.mu.Lock()
	if e := opCtx.Err(); e != nil {
		fail(w, e)
		return
	}
	if current := s.state.Accounts[id]; current == nil || current.ConnectionID != connectionID {
		fail(w, &APIError{409, "CONNECTION_ENDED", 0})
		return
	}
	if e := s.accountError(id, true); e != nil {
		fail(w, e)
		return
	}
	if opCtx.Err() != nil {
		fail(w, opCtx.Err())
		return
	}
	commitCtx, finish, e := s.operationContext(opCtx)
	if e != nil {
		fail(w, e)
		return
	}
	defer finish()
	if err != nil {
		s.commitHTTP(w, r.WithContext(commitCtx), 0, nil, s.accountFailure(id, err))
		return
	}
	s.commitHTTP(w, r.WithContext(commitCtx), 200, out, nil)
}
