package platform_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestNewDatabaseHasNoServiceAccounts(t *testing.T) {
	_, s := fixture(t)
	token, _ := login(t, s, "admin")
	var accounts []any
	if readJSON(t, s.URL, "/api/accounts", token, &accounts) != 200 || len(accounts) != 0 {
		t.Fatalf("new database manufactured %d service accounts", len(accounts))
	}
}

func TestUploadFormatErrorsDoNotCreateAccountsOrRequireGateway(t *testing.T) {
	_, s := fixture(t)
	token, _ := login(t, s, "admin")
	viewer, _ := login(t, s, "viewer")
	parts := []uploadPart{{"unpaired.session", "SQLite format 3\x00input"}, {"duplicate.session", "input one"}, {"duplicate.session", "input two"}, {"duplicate.json", `{}`}, {"format.session", "not a SQLite file"}, {"format.json", `{}`}, {"credentials.session", "SQLite format 3\x00input"}, {"credentials.json", `{"api_id":12345}`}}
	st, b := uploadAccounts(t, s, viewer, parts, "")
	if st != 403 {
		t.Fatal("viewer upload permitted", st, b)
	}
	st, b = uploadAccounts(t, s, token, parts, "")
	if st != 200 || len(b["results"].([]any)) != 4 {
		t.Fatal(st, b)
	}
	for _, v := range b["results"].([]any) {
		if v.(map[string]any)["success"] != false {
			t.Fatal("invalid input manufactured account", b)
		}
	}
	var accounts []any
	readJSON(t, s.URL, "/api/accounts", token, &accounts)
	if len(accounts) != 0 {
		t.Fatal("invalid format created accounts")
	}
}

func TestCompletedImportRequiresCredentialEvidenceAndPreservesBusinessDataOnOutage(t *testing.T) {
	_, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: "http://127.0.0.1:0"})
	token, _ := login(t, s, "admin")
	// This is an invalid Session parser input. No gateway runs, and neither upload
	// can report a successful Session registration or Telegram operation.
	parts := []uploadPart{{"legacy.session", "SQLite format 3\x00parser-input-only"}, {"legacy.json", `{"api_id":12345,"api_hash":"0123456789abcdef0123456789abcdef"}`}}
	st, b := uploadAccounts(t, s, token, parts, "")
	if st != 200 {
		t.Fatal(st, b)
	}
	result := b["results"].([]any)[0].(map[string]any)
	if result["success"] != false {
		t.Fatal("unavailable gateway reported registration success")
	}
	id := result["accountId"].(string)
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	for _, query := range []string{`INSERT INTO accounts(id,phone,first_name) VALUES($1,'legacy input phone','Preserved')`, `UPDATE account_imports SET completed=true WHERE account_id=$1`, `INSERT INTO account_logs(account_id,actor,action,result) VALUES($1,'database input','historical_input','success')`, `INSERT INTO account_group_snapshots(account_id,items) VALUES($1,'[]')`} {
		if _, e = pool.Exec(context.Background(), query, id); e != nil {
			t.Fatal(e)
		}
	}
	st, b = uploadAccounts(t, s, token, parts, "")
	if st != 200 {
		t.Fatal(st, b)
	}
	result = b["results"].([]any)[0].(map[string]any)
	if result["success"] != false || result["error"].(map[string]any)["code"] != "GATEWAY_UNAVAILABLE" {
		t.Fatalf("completed import bypassed real credential evidence: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/"+id, token, "")
	if st != 200 || b["phone"] != "legacy input phone" || b["firstName"] != "Preserved" {
		t.Fatalf("evidence outage changed business profile: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/"+id+"/logs?action=historical_input", token, "")
	if st != 200 || len(b["items"].([]any)) != 1 {
		t.Fatalf("historical log lost: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/"+id+"/joined-groups", token, "")
	if st != 200 || b["lastSyncedAt"] == nil {
		t.Fatalf("historical snapshot lost: %d %v", st, b)
	}
}
