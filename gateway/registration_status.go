package gateway

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// RegistrationStatus describes local credential readiness, not Telegram authorization.
type RegistrationStatus struct {
	AccountID string  `json:"accountId"`
	Status    string  `json:"status"`
	Source    *string `json:"source"`
}

func validDurableConfig(c AccountConfig) bool {
	_, err := hex.DecodeString(c.APIHash)
	return c.ID != "" && !strings.ContainsAny(c.ID, "/\\\x00") && len(c.ID) <= 128 && c.APIID > 0 && c.APIID <= 2147483647 && len(c.APIHash) == 32 && err == nil && c.SessionPath == "" && c.SessionFormat == "" && c.Passcode == ""
}

func configFingerprint(c AccountConfig) string {
	b, _ := json.Marshal(c)
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

func (p *PostgresStore) RegistrationStatus(ctx context.Context, id string) (RegistrationStatus, error) {
	return inspectRegistration(ctx, id, p.DB.QueryRowContext(ctx, registrationQuery, id))
}

const registrationQuery = `SELECT c.account_id,c.config,c.authorization_fingerprint,c.configuration_fingerprint,c.source,s.account_id,s.data
	FROM (SELECT $1::text AS id) a
	LEFT JOIN gateway_account_configs c ON c.account_id=a.id
	LEFT JOIN gateway_sessions s ON s.account_id=a.id`

func inspectRegistration(ctx context.Context, id string, row *sql.Row) (RegistrationStatus, error) {
	out := RegistrationStatus{AccountID: id, Status: "absent"}
	var config, data []byte
	var configID, sessionID, authorization, configuration, source sql.NullString
	err := row.Scan(&configID, &config, &authorization, &configuration, &source, &sessionID, &data)
	if err != nil {
		return out, &APIError{503, "GATEWAY_UNAVAILABLE", 0}
	}
	if !configID.Valid && !sessionID.Valid {
		return out, nil
	}
	out.Status = "incomplete"
	if source.String == "upload" || source.String == "manifest" {
		out.Source = &source.String
	}
	var c AccountConfig
	if !configID.Valid || !sessionID.Valid || json.Unmarshal(config, &c) != nil || c.ID != id || !validDurableConfig(c) || configFingerprint(c) != configuration.String || out.Source == nil {
		return out, nil
	}
	fingerprint, err := authorizationFingerprint(ctx, data)
	if err == nil && fingerprint == authorization.String {
		out.Status = "ready"
	}
	return out, nil
}

func (s *Service) registrationHTTP(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.RegistrationStatus(r.Context(), r.PathValue("accountId"))
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, out)
}
