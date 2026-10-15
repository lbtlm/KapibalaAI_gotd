package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/session"
)

func authorizationFingerprint(ctx context.Context, data []byte) (string, error) {
	memory := &session.StorageMemory{}
	if err := memory.StoreSession(ctx, data); err != nil {
		return "", err
	}
	auth, err := (&session.Loader{Storage: memory}).Load(ctx)
	if err != nil {
		return "", err
	}
	if len(auth.AuthKey) != 256 || len(auth.AuthKeyID) != 8 || auth.DC <= 0 || auth.Addr == "" {
		return "", errors.New("invalid authorization data")
	}
	var key crypto.Key
	copy(key[:], auth.AuthKey)
	withID := key.WithID()
	if !bytes.Equal(withID.ID[:], auth.AuthKeyID) {
		return "", errors.New("invalid authorization key identity")
	}
	fingerprint := sha256.Sum256(auth.AuthKey)
	return hex.EncodeToString(fingerprint[:]), nil
}

// authorizationOwners includes legacy/orphan Sessions before they have config rows.
// The transaction holds each Session stable until the registration/adopt commits.
func authorizationOwners(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT account_id,data FROM gateway_sessions FOR SHARE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners, identities := map[string]string{}, map[string]string{}
	for rows.Next() {
		var id string
		var data []byte
		if err = rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		authorization, e := authorizationFingerprint(ctx, data)
		if e != nil {
			return nil, &APIError{503, "CREDENTIALS_INCOMPLETE", 0}
		}
		if identities[authorization] != "" {
			return nil, &APIError{409, "ACCOUNT_IMPORT_CONFLICT", 0}
		}
		owners[id], identities[authorization] = authorization, id
	}
	return owners, rows.Err()
}

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
			status, e := inspectRegistration(ctx, id, tx.QueryRowContext(ctx, registrationQuery, id))
			if e != nil {
				return e
			}
			if status.Status == "ready" {
				return tx.Commit()
			}
		}
		return &APIError{409, "ACCOUNT_IMPORT_CONFLICT", 0}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	owners, err := authorizationOwners(ctx, tx)
	if err != nil {
		return err
	}
	for id, auth := range owners {
		if id == c.ID || auth == identity.Authorization {
			return &APIError{409, "ACCOUNT_IMPORT_CONFLICT", 0}
		}
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
	if err := a.service.store.RegisterAccount(ctx, c, data, identity); err != nil {
		return err
	}
	a.configs[c.ID] = c
	return nil
}
