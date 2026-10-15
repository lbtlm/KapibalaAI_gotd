package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CheckAccountRetired fences delayed imports and explicit connection requests.
func (p *PostgresStore) CheckAccountRetired(ctx context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLeaseLocked(ctx); err != nil {
		return err
	}
	var retired bool
	if err := p.lease.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM gateway_retired_accounts WHERE account_id=$1)", id).Scan(&retired); err != nil {
		return err
	}
	if retired {
		return &APIError{409, "ACCOUNT_RETIRED", 0}
	}
	return nil
}

// DeleteAccount atomically retires an identity and removes its local credentials.
// An uncertain commit invalidates this writer; reconstruction resolves the result.
func (p *PostgresStore) DeleteAccount(ctx context.Context, id string, snapshot []byte) (err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	leaseCtx, stopLease := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	err = p.checkLeaseLocked(leaseCtx)
	stopLease()
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	tx, err := p.lease.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committing := false
	defer func() {
		if err == nil {
			return
		}
		rollbackErr := tx.Rollback()
		if committing || rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			p.lost = true
			err = fmt.Errorf("%w: %v", ErrWriterLeaseLost, err)
			return
		}
		// Cancellation can roll back a transaction without losing the writer.
		// Verify the fixed connection before allowing unrelated accounts to run.
		leaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if leaseErr := p.checkLeaseLocked(leaseCtx); leaseErr != nil {
			err = leaseErr
		}
	}()
	if _, err = tx.ExecContext(ctx, "INSERT INTO gateway_retired_accounts(account_id) VALUES($1) ON CONFLICT DO NOTHING", id); err != nil {
		return err
	}
	for _, table := range []string{"gateway_account_configs", "gateway_sessions", "gateway_update_state", "gateway_channel_state"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE account_id=$1", id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO gateway_state(id,data) VALUES(1,$1) ON CONFLICT(id) DO UPDATE SET data=EXCLUDED.data", snapshot); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	committing = true
	return tx.Commit()
}

// deletionBlocker treats unknown managed references conservatively. Only a
// confirmed leave distinguishes historical references from terminal cleanup.
func (s *Service) deletionBlocker(id string) string {
	a := s.state.Accounts[id]
	if a != nil {
		if a.ConnectionID != "" {
			return "active_connection"
		}
		switch a.Status {
		case "idle", "offline", "session_expired", "suspended":
		default:
			return "active_connection"
		}
	}
	for key, e := range s.state.Effects {
		if e.State != "sent" && e.State != "completed" && e.State != "failed" && (e.AccountID == id || strings.HasPrefix(key, "promote:") && e.TargetID == id) {
			return "active_effect"
		}
	}
	for _, g := range s.state.Groups {
		if g.Recovery != nil && g.Recovery.AccountID == id && g.Recovery.Status == "pending" {
			return "active_effect"
		}
		if g.LeftAccounts[id] && (a == nil || a.PlatformID == "" || !g.Members[a.PlatformID]) {
			continue
		}
		_, hash := g.AccessHashes[id]
		_, history := g.HistoryThrough[id]
		_, verified := g.VerifiedHistoryThrough[id]
		if g.Owner == id || g.Admins[id] || hash || history || verified || g.Recovery != nil && g.Recovery.AccountID == id || a != nil && a.PlatformID != "" && (g.Members[a.PlatformID] || g.MemberRoles[a.PlatformID] != "") {
			return "managed_group"
		}
	}
	return ""
}

func (g *group) confirmAccountJoin(id string) { delete(g.LeftAccounts, id) }

func (g *group) confirmAccountLeave(id string) {
	if g.LeftAccounts == nil {
		g.LeftAccounts = map[string]bool{}
	}
	g.LeftAccounts[id] = true
}

func (g *group) confirmObservedJoins(accounts map[string]*account) {
	for id := range g.LeftAccounts {
		if a := accounts[id]; a != nil && a.PlatformID != "" && g.Members[a.PlatformID] {
			g.confirmAccountJoin(id)
		}
	}
}

func (s *Service) deleteAccountHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("accountId")
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\\x00") {
		fail(w, &APIError{400, "INVALID_INPUT", 0})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, stop, err := s.operationContext(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	defer stop()
	if reason := s.deletionBlocker(id); reason != "" {
		respond(w, 409, map[string]string{"code": "ACCOUNT_IN_USE", "reason": reason})
		return
	}
	a := s.cfg.Telegram
	// Keep deletion admission bounded while holding Service.mu.
	if !a.mu.TryLock() {
		fail(w, &APIError{503, "GATEWAY_UNAVAILABLE", 0})
		return
	}
	defer a.mu.Unlock()
	if live := a.sessions[id]; live != nil {
		select {
		case <-live.done:
		default:
			respond(w, 409, map[string]string{"code": "ACCOUNT_IN_USE", "reason": "active_connection"})
			return
		}
	}
	if a.recovering[id] {
		respond(w, 409, map[string]string{"code": "ACCOUNT_IN_USE", "reason": "active_effect"})
		return
	}
	b, err := json.Marshal(s.state)
	var next state
	if err == nil {
		err = json.Unmarshal(b, &next)
	}
	if err != nil {
		fail(w, err)
		return
	}
	delete(next.Accounts, id)
	for _, g := range next.Groups {
		if !g.LeftAccounts[id] {
			continue
		}
		if g.Owner == id {
			g.Owner = ""
		}
		delete(g.Admins, id)
		delete(g.AccessHashes, id)
		delete(g.HistoryThrough, id)
		delete(g.VerifiedHistoryThrough, id)
		if old := s.state.Accounts[id]; old != nil && old.PlatformID != "" {
			delete(g.MemberRoles, old.PlatformID)
		}
		delete(g.LeftAccounts, id)
		if g.Recovery != nil && g.Recovery.AccountID == id {
			g.Recovery.AccountID = ""
		}
	}
	b, err = json.Marshal(next)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = s.store.DeleteAccount(ctx, id, b)
	}
	if err != nil {
		if errors.Is(err, ErrWriterLeaseLost) {
			s.cancel()
		}
		fail(w, err)
		return
	}
	s.state = next
	delete(a.configs, id)
	delete(a.sessions, id)
	w.WriteHeader(http.StatusNoContent)
}
