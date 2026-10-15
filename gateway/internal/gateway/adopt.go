package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	sessionimport "github.com/lbtlm/KapibalaAI_gotd/gateway/internal/session"
	"strconv"
	"strings"
)

// AdoptAccounts is an explicit offline transition for existing manifest accounts.
func (p *PostgresStore) AdoptAccounts(ctx context.Context, configs []AccountConfig) (int, error) {
	if len(configs) == 0 {
		return 0, &APIError{400, "ADOPT_ACCOUNTS_REQUIRED", 0}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLeaseLocked(ctx); err != nil {
		return 0, err
	}
	tx, err := p.lease.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var snapshot []byte
	if err = tx.QueryRowContext(ctx, "SELECT data FROM gateway_state WHERE id=1 FOR UPDATE").Scan(&snapshot); err != nil {
		if err == sql.ErrNoRows {
			return 0, &APIError{409, "ADOPT_EXISTING_TELEGRAM_STATE_REQUIRED", 0}
		}
		return 0, err
	}
	var old state
	if json.Unmarshal(snapshot, &old) != nil || old.Mode != "telegram" {
		return 0, &APIError{409, "ADOPT_EXISTING_TELEGRAM_STATE_REQUIRED", 0}
	}
	// Every existing authorization participates in deduplication, including
	// aliases not selected by this command and not yet in the config registry.
	byAccount, err := authorizationOwners(ctx, tx)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	adopted := 0
	for _, c := range configs {
		if seen[c.ID] || old.Accounts[c.ID] == nil || byAccount[c.ID] == "" {
			return 0, &APIError{409, "ADOPT_EXISTING_ACCOUNT_REQUIRED", 0}
		}
		seen[c.ID] = true
		pid, e := strconv.ParseInt(old.Accounts[c.ID].PlatformID, 10, 64)
		if e != nil || pid <= 0 {
			return 0, &APIError{409, "ADOPT_EXISTING_IDENTITY_REQUIRED", 0}
		}
		source, e := sessionimport.ImportSession(c.SessionPath, c.SessionFormat, c.Passcode)
		if e != nil {
			return 0, &APIError{409, "ADOPT_SOURCE_UNAVAILABLE", 0}
		}
		authorization, e := authorizationFingerprint(ctx, source)
		if e != nil || authorization != byAccount[c.ID] {
			return 0, &APIError{409, "ADOPT_AUTHORIZATION_CONFLICT", 0}
		}
		c.SessionPath, c.SessionFormat, c.Passcode = "", "", ""
		c.APIHash = strings.ToLower(c.APIHash)
		if !validDurableConfig(c) {
			return 0, &APIError{400, "ADOPT_INVALID_CONFIG", 0}
		}
		configuration := configFingerprint(c)
		var id, auth, fingerprint string
		var existing []byte
		err = tx.QueryRowContext(ctx, "SELECT account_id,authorization_fingerprint,configuration_fingerprint,config FROM gateway_account_configs WHERE account_id=$1 OR authorization_fingerprint=$2", c.ID, authorization).Scan(&id, &auth, &fingerprint, &existing)
		if err == nil {
			var configured AccountConfig
			if id != c.ID || auth != authorization || fingerprint != configuration || json.Unmarshal(existing, &configured) != nil || !validDurableConfig(configured) || configured != c {
				return 0, &APIError{409, "ADOPT_CONFIG_CONFLICT", 0}
			}
			continue
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
		encoded, e := json.Marshal(c)
		if e != nil {
			return 0, e
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO gateway_account_configs(account_id,authorization_fingerprint,configuration_fingerprint,config,source) VALUES($1,$2,$3,$4,'manifest')", c.ID, authorization, configuration, encoded); err != nil {
			return 0, err
		}
		adopted++
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return adopted, nil
}
