package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
)

func legacyAccount(t *testing.T, p *gateway.PostgresStore, id string, seed byte) gateway.AccountConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.session")
	if err := os.WriteFile(path, structuralSession(t, seed), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := gateway.ImportSession(path, "TelethonSQLite", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.DB.Exec("INSERT INTO gateway_sessions(account_id,data) VALUES($1,$2)", id, data); err != nil {
		t.Fatal(err)
	}
	return gateway.AccountConfig{ID: id, APIID: 123, APIHash: "0123456789abcdef0123456789abcdef", SessionPath: path, SessionFormat: "TelethonSQLite"}
}

func legacySnapshot(t *testing.T, p *gateway.PostgresStore, mode string, ids ...string) []byte {
	t.Helper()
	accounts := map[string]any{}
	for _, id := range ids {
		accounts[id] = map[string]any{"status": "offline", "platformId": "12345"}
	}
	b, err := json.Marshal(map[string]any{"mode": mode, "accounts": accounts, "groups": map[string]any{}, "nextEvent": 41, "events": []any{map[string]any{"eventId": 41, "type": "account_status", "data": map[string]any{"eventId": 41, "type": "account_status", "accountId": "preserved", "status": "offline"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Save(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPostgresOfflineAdoptAddsOnlyConfigAndSurvivesReconstruction(t *testing.T) {
	p := isolatedStore(t)
	c := legacyAccount(t, p, "legacy-alias", 41)
	before := legacySnapshot(t, p, "telegram", c.ID)
	var sessionBefore []byte
	if err := p.DB.QueryRow("SELECT data FROM gateway_sessions WHERE account_id=$1", c.ID).Scan(&sessionBefore); err != nil {
		t.Fatal(err)
	}
	count, err := p.AdoptAccounts(context.Background(), []gateway.AccountConfig{c})
	if err != nil || count != 1 {
		t.Fatalf("offline adopt failed: count=%d error=%v", count, err)
	}
	after, err := p.Load(context.Background())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("adopt changed the persisted state/event stream")
	}
	var sessionAfter []byte
	if err = p.DB.QueryRow("SELECT data FROM gateway_sessions WHERE account_id=$1", c.ID).Scan(&sessionAfter); err != nil || !bytes.Equal(sessionBefore, sessionAfter) {
		t.Fatal("adopt replaced the existing Session")
	}
	status, err := p.RegistrationStatus(context.Background(), c.ID)
	if err != nil || status.Status != "ready" || status.Source == nil || *status.Source != "manifest" {
		t.Fatal("adopt did not record local readiness and manifest provenance")
	}
	count, err = p.AdoptAccounts(context.Background(), []gateway.AccountConfig{c})
	if err != nil || count != 0 {
		t.Fatal("identical offline adopt must be idempotent")
	}
	if err = os.Remove(c.SessionPath); err != nil {
		t.Fatal(err)
	}
	p.Close()
	p, err = gateway.OpenPostgres(context.Background(), os.Getenv("GATEWAY_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	s, server := realServer(t, p)
	defer s.Close()
	defer server.Close()
	out := request(t, server, "GET", "/accounts/"+c.ID+"/registration", nil, 200)
	if out["status"] != "ready" || out["source"] != "manifest" {
		t.Fatal("reconstruction needs the removed manifest Session")
	}
	request(t, server, "POST", "/accounts/"+c.ID+"/check", nil, 409)
}

func TestPostgresOfflineAdoptRejectsUnsafeSourcesAtomically(t *testing.T) {
	for _, window := range []string{"fake-state", "unknown-state", "missing-session", "different-authorization", "source-missing", "duplicate-authorization", "existing-config-conflict"} {
		t.Run(window, func(t *testing.T) {
			p := isolatedStore(t)
			first := legacyAccount(t, p, "first", 61)
			second := legacyAccount(t, p, "second", 62)
			mode := "telegram"
			if window == "fake-state" {
				mode = "fake"
			}
			if window == "unknown-state" {
				mode = "unknown"
			}
			before := legacySnapshot(t, p, mode, first.ID, second.ID)
			switch window {
			case "missing-session":
				_, _ = p.DB.Exec("DELETE FROM gateway_sessions WHERE account_id=$1", second.ID)
			case "different-authorization":
				if err := os.WriteFile(second.SessionPath, structuralSession(t, 63), 0600); err != nil {
					t.Fatal(err)
				}
			case "source-missing":
				if err := os.Remove(second.SessionPath); err != nil {
					t.Fatal(err)
				}
			case "duplicate-authorization":
				_, _ = p.DB.Exec("UPDATE gateway_sessions SET data=(SELECT data FROM gateway_sessions WHERE account_id=$1) WHERE account_id=$2", first.ID, second.ID)
			case "existing-config-conflict":
				if count, err := p.AdoptAccounts(context.Background(), []gateway.AccountConfig{second}); err != nil || count != 1 {
					t.Fatal("precondition adopt failed")
				}
				second.APIID = 456
			}
			var baseline int
			if err := p.DB.QueryRow("SELECT count(*) FROM gateway_account_configs").Scan(&baseline); err != nil {
				t.Fatal(err)
			}
			if count, err := p.AdoptAccounts(context.Background(), []gateway.AccountConfig{first, second}); err == nil || count != 0 {
				t.Fatal("unsafe adopt was accepted")
			}
			var afterCount int
			if err := p.DB.QueryRow("SELECT count(*) FROM gateway_account_configs").Scan(&afterCount); err != nil || afterCount != baseline {
				t.Fatal("partial adopt escaped its transaction")
			}
			after, err := p.Load(context.Background())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("refused adopt changed state/events")
			}
		})
	}
}
