package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/gotd/td/session"
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

func (s *Service) registrationConflict(id string, identity registration) bool {
	for other, old := range s.state.Registrations {
		if other == id && old != identity || other != id && old.Authorization == identity.Authorization {
			return true
		}
	}
	_, exists := s.state.Registrations[id]
	return !exists && s.state.Accounts[id] != nil && s.cfg.Mode != "telegram"
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
	memory := &session.StorageMemory{}
	_ = memory.StoreSession(r.Context(), converted)
	auth, err := (&session.Loader{Storage: memory}).Load(r.Context())
	if err != nil {
		fail(w, &APIError{400, "INVALID_SESSION", 0})
		return
	}
	c := AccountConfig{ID: in.AccountID, APIID: in.APIID, APIHash: strings.ToLower(in.APIHash), DeviceModel: in.DeviceModel, SystemVersion: in.SystemVersion, AppVersion: in.AppVersion}
	b, _ := json.Marshal(c)
	authHash, configHash := sha256.Sum256(auth.AuthKey), sha256.Sum256(b)
	identity := registration{hex.EncodeToString(authHash[:]), hex.EncodeToString(configHash[:])}
	s.mu.Lock()
	defer s.mu.Unlock()
	opCtx, stop, err := s.operationContext(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	defer stop()
	if s.state.Registrations == nil {
		s.state.Registrations = map[string]registration{}
	}
	if s.registrationConflict(in.AccountID, identity) {
		fail(w, &APIError{409, "ACCOUNT_IMPORT_CONFLICT", 0})
		return
	}
	if a, ok := s.cfg.Telegram.(interface {
		Register(context.Context, AccountConfig, []byte, registration) error
	}); ok {
		s.mu.Unlock()
		err = a.Register(opCtx, c, converted, identity)
		s.mu.Lock()
		if err != nil {
			fail(w, err)
			return
		}
	} else if s.cfg.Mode == "telegram" {
		fail(w, &APIError{503, "GATEWAY_UNAVAILABLE", 0})
		return
	}
	commitCtx, finish, err := s.operationContext(opCtx)
	if err != nil {
		fail(w, err)
		return
	}
	defer finish()
	if s.state.Registrations == nil {
		s.state.Registrations = map[string]registration{}
	}
	if s.registrationConflict(in.AccountID, identity) {
		fail(w, &APIError{409, "ACCOUNT_IMPORT_CONFLICT", 0})
		return
	}
	s.state.Registrations[in.AccountID] = identity
	if s.state.Accounts[in.AccountID] == nil {
		platformID := ""
		if s.cfg.Mode == "fake" {
			platformID = "fake-" + identity.Authorization[:16]
		}
		s.state.Accounts[in.AccountID] = &account{Status: "idle", PlatformID: platformID}
	}
	s.commitHTTP(w, r.WithContext(commitCtx), 200, map[string]string{"accountId": in.AccountID, "status": "idle"}, nil)
}

func (s *Service) inspectHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("accountId")
	op := "check"
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
	if f := s.consumeFault(op, id, ""); f != nil {
		s.commitHTTP(w, r, 0, nil, s.accountFailure(id, faultError(f)))
		return
	}
	var out any
	if a, ok := s.cfg.Telegram.(interface {
		Inspect(context.Context, string, string) (any, error)
	}); ok {
		connectionID := s.state.Accounts[id].ConnectionID
		s.mu.Unlock()
		out, err = a.Inspect(opCtx, id, op)
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
	} else if s.cfg.Mode == "telegram" {
		err = &APIError{503, "GATEWAY_UNAVAILABLE", 0}
	} else if op == "check" {
		acct := s.state.Accounts[id]
		out = AccountProfile{Source: "simulated", PlatformUserID: acct.PlatformID, Username: optionalString(strings.ReplaceAll(id, "-", "_")), FirstName: optionalString("Test"), LastName: optionalString(id)}
	} else {
		items := []JoinedGroup{}
		acct := s.state.Accounts[id]
		for gid, g := range s.state.Groups {
			if g.Members[acct.PlatformID] {
				role := "member"
				if g.Owner == id {
					role = "creator"
				} else if g.Admins[id] {
					role = "admin"
				}
				items = append(items, JoinedGroup{ID: gid, Title: gid, Type: "supergroup", Role: role})
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		out = map[string]any{"items": items}
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
