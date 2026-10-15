package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// RegisterAccount commits imported authorization and configuration together on
// the connection that owns the writer lease. Existing Sessions are never updated.
func (p *PostgresStore) RegisterAccount(ctx context.Context, c AccountConfig, data []byte, identity registration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLeaseLocked(ctx); err != nil {
		return err
	}
	tx, err := p.lease.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, authorization, configuration string
	err = tx.QueryRowContext(ctx, "SELECT account_id,authorization_fingerprint,configuration_fingerprint FROM gateway_account_configs WHERE account_id=$1 OR authorization_fingerprint=$2", c.ID, identity.Authorization).Scan(&id, &authorization, &configuration)
	if err == nil {
		if id == c.ID && authorization == identity.Authorization && configuration == identity.Configuration {
			return tx.Commit()
		}
		return &APIError{409, "ACCOUNT_IMPORT_CONFLICT", 0}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM gateway_sessions WHERE account_id=$1)", c.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return &APIError{409, "ACCOUNT_IMPORT_CONFLICT", 0}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO gateway_account_configs(account_id,authorization_fingerprint,configuration_fingerprint,config) VALUES($1,$2,$3,$4)", c.ID, identity.Authorization, identity.Configuration, b); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO gateway_sessions(account_id,data) VALUES($1,$2)", c.ID, data); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *TelegramAdapter) Register(ctx context.Context, c AccountConfig, data []byte, identity registration) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if old, ok := a.configs[c.ID]; ok && old.SessionPath != "" {
		return &APIError{409, "ACCOUNT_IMPORT_CONFLICT", 0}
	}
	owner, ok := a.service.store.(interface {
		RegisterAccount(context.Context, AccountConfig, []byte, registration) error
	})
	if !ok {
		return &APIError{503, "GATEWAY_UNAVAILABLE", 0}
	}
	if err := owner.RegisterAccount(ctx, c, data, identity); err != nil {
		return err
	}
	a.configs[c.ID] = c
	return nil
}
