package gateway_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.Contains(parsed.Path, "test") && !strings.Contains(parsed.Path, "inspection") {
		t.Fatal("gateway tests require an explicit isolated test/inspection database URL")
	}
	return dsn
}

func isolatedStore(t *testing.T) *gateway.PostgresStore {
	t.Helper()
	dsn := testDSN(t)
	p, err := gateway.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err = p.DB.QueryRow("SELECT current_database()").Scan(&name); err != nil || !strings.Contains(name, "test") && !strings.Contains(name, "inspection") {
		p.Close()
		t.Fatal("gateway tests require an isolated test/inspection database")
	}
	if _, err = p.DB.Exec("TRUNCATE gateway_state,gateway_account_configs,gateway_sessions,gateway_update_state,gateway_channel_state"); err != nil {
		p.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

// Structural input exercises storage and parsing only; it is never connected.
func structuralSession(t *testing.T, seed byte) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.session")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE sessions (dc_id INTEGER, server_address TEXT, port INTEGER, auth_key BLOB)`); err == nil {
		key := bytes.Repeat([]byte{seed}, 256)
		_, err = db.Exec(`INSERT INTO sessions VALUES (2,'192.0.2.1',443,?)`, key)
	}
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal("structural input creation failed")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func registrationInput(id string, data []byte) map[string]any {
	return map[string]any{"accountId": id, "session": base64.StdEncoding.EncodeToString(data), "apiId": 123, "apiHash": "0123456789abcdef0123456789abcdef"}
}

func realServer(t *testing.T, p *gateway.PostgresStore) (*gateway.Service, *httptest.Server) {
	t.Helper()
	a, err := gateway.NewTelegramAdapter(context.Background(), p.DB)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gateway.New(context.Background(), gateway.Config{Store: p, Telegram: a})
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	return s, httptest.NewServer(s)
}

func request(t *testing.T, server *httptest.Server, method, path string, body any, status int) map[string]any {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(method, server.URL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("%s %s: status %d, want %d", method, path, response.StatusCode, status)
	}
	var out map[string]any
	_ = json.NewDecoder(response.Body).Decode(&out)
	return out
}

func TestRealOnlyGatewayStartsEmptyAndRejectsSimulation(t *testing.T) {
	p := isolatedStore(t)
	a, err := gateway.NewTelegramAdapter(context.Background(), p.DB)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gateway.New(context.Background(), gateway.Config{Store: p, Telegram: a})
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	server := httptest.NewServer(s)
	defer server.Close()
	defer s.Close()
	if out := request(t, server, "GET", "/health", nil, 200); out["mode"] != "telegram" {
		t.Fatal("health must report real Telegram mode")
	}
	for _, id := range []string{"account-1", "account-2", "account-3"} {
		out := request(t, server, "GET", "/accounts/"+id+"/registration", nil, 200)
		if out["status"] != "absent" || out["source"] != nil {
			t.Fatal("new database must have no account credentials")
		}
		request(t, server, "POST", "/accounts/"+id+"/connect", nil, 404)
	}
	request(t, server, "POST", "/admin/reset", nil, 404)
}

func TestPostgresRegistrationInventoryRejectsIncompleteReplay(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer server.Close()
	defer s.Close()
	input := registrationInput("stored-account", structuralSession(t, 19))
	request(t, server, "POST", "/accounts/register", input, 200)
	changed := registrationInput("stored-account", structuralSession(t, 19))
	changed["apiId"] = 456
	request(t, server, "POST", "/accounts/register", changed, 409)
	out := request(t, server, "GET", "/accounts/stored-account/registration", nil, 200)
	if out["status"] != "ready" || out["source"] != "upload" || len(out) != 3 {
		t.Fatal("inventory must expose only local readiness and provenance")
	}
	request(t, server, "POST", "/accounts/stored-account/check", nil, 409)
	request(t, server, "GET", "/accounts/stored-account/joined-groups", nil, 409)
	request(t, server, "POST", "/accounts/register", input, 200)
	request(t, server, "POST", "/accounts/register", registrationInput("stored-account", structuralSession(t, 20)), 409)
	request(t, server, "POST", "/accounts/register", registrationInput("another-alias", structuralSession(t, 19)), 409)
	if _, err := p.DB.Exec("UPDATE gateway_sessions SET data=$1 WHERE account_id=$2", []byte("corrupt"), "stored-account"); err != nil {
		t.Fatal(err)
	}
	out = request(t, server, "GET", "/accounts/stored-account/registration", nil, 200)
	if out["status"] != "incomplete" {
		t.Fatal("corrupt durable Session was accepted")
	}
	request(t, server, "POST", "/accounts/register", input, 409)
}

func TestPostgresInventoryRejectsMissingOrInvalidCredentialSides(t *testing.T) {
	for _, window := range []string{"missing-session", "corrupt-config", "mismatched-authorization", "mismatched-config"} {
		t.Run(window, func(t *testing.T) {
			p := isolatedStore(t)
			s, server := realServer(t, p)
			defer s.Close()
			defer server.Close()
			input := registrationInput("inventory-input", structuralSession(t, 91))
			request(t, server, "POST", "/accounts/register", input, 200)
			var err error
			switch window {
			case "missing-session":
				_, err = p.DB.Exec("DELETE FROM gateway_sessions WHERE account_id=$1", "inventory-input")
			case "corrupt-config":
				_, err = p.DB.Exec("UPDATE gateway_account_configs SET config=$1 WHERE account_id=$2", []byte("invalid"), "inventory-input")
			case "mismatched-authorization":
				_, err = p.DB.Exec("UPDATE gateway_account_configs SET authorization_fingerprint=$1 WHERE account_id=$2", "different", "inventory-input")
			case "mismatched-config":
				_, err = p.DB.Exec("UPDATE gateway_account_configs SET configuration_fingerprint=$1 WHERE account_id=$2", "different", "inventory-input")
			}
			if err != nil {
				t.Fatal(err)
			}
			out := request(t, server, "GET", "/accounts/inventory-input/registration", nil, 200)
			if out["status"] != "incomplete" || len(out) != 3 {
				t.Fatal("invalid credential side was not reported as incomplete")
			}
			request(t, server, "POST", "/accounts/register", input, 409)
		})
	}
}

func TestPostgresUnregisteredSessionCannotBeAliasedByImport(t *testing.T) {
	p := isolatedStore(t)
	c := legacyAccount(t, p, "orphaned-authorization", 57)
	s, server := realServer(t, p)
	defer s.Close()
	defer server.Close()
	out := request(t, server, "GET", "/accounts/"+c.ID+"/registration", nil, 200)
	if out["status"] != "incomplete" || out["source"] != nil {
		t.Fatal("a Session without config must not be absent or ready")
	}
	request(t, server, "POST", "/accounts/register", registrationInput("different-alias", structuralSession(t, 57)), 409)
	request(t, server, "POST", "/accounts/register", registrationInput("independent-input", structuralSession(t, 58)), 200)
}

func TestPostgresStartupRefusesUnknownOrMissingCredentialProvenance(t *testing.T) {
	for _, mode := range []string{"fake", "unknown", "", "telegram"} {
		t.Run("persisted-mode-"+mode, func(t *testing.T) {
			p := isolatedStore(t)
			before := legacySnapshot(t, p, mode, "unmigrated-alias")
			a, err := gateway.NewTelegramAdapter(context.Background(), p.DB)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			s, err := gateway.New(context.Background(), gateway.Config{Store: p, Telegram: a})
			if err == nil {
				s.Close()
				t.Fatal("unknown provenance or missing durable config was accepted")
			}
			after, err := p.Load(context.Background())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("refused startup modified legacy data")
			}
		})
	}
}

func TestPostgresLegacyOnlineWithoutConnectionIDStopsBeforeResume(t *testing.T) {
	p := isolatedStore(t)
	c := legacyAccount(t, p, "legacy-online", 71)
	legacySnapshot(t, p, "telegram", c.ID)
	if _, err := p.AdoptAccounts(context.Background(), []gateway.AccountConfig{c}); err != nil {
		t.Fatal(err)
	}
	b, err := p.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err = json.Unmarshal(b, &state); err != nil {
		t.Fatal(err)
	}
	a := state["accounts"].(map[string]any)[c.ID].(map[string]any)
	a["status"] = "online"
	a["endedConnections"] = map[string]bool{"ended-generation": true}
	b, _ = json.Marshal(state)
	if err = p.Save(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	s, server := realServer(t, p)
	defer s.Close()
	defer server.Close()
	request(t, server, "POST", "/accounts/"+c.ID+"/check", nil, 409)
	request(t, server, "POST", "/accounts/"+c.ID+"/connect", map[string]string{"connectionId": "ended-generation"}, 409)
	b, err = p.Load(context.Background())
	if err != nil || json.Unmarshal(b, &state) != nil {
		t.Fatal("cannot read upgraded snapshot")
	}
	a = state["accounts"].(map[string]any)[c.ID].(map[string]any)
	if a["status"] != "offline" || state["nextEvent"] != float64(41) {
		t.Fatal("legacy resume was not stopped before a platform connection")
	}
}

func TestPostgresRegistrationSurvivesRestartAndRejectsConcurrentPayload(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	input := registrationInput("durable-upload", structuralSession(t, 81))
	request(t, server, "POST", "/accounts/register", input, 200)
	server.Close()
	s.Close()
	var err error
	p, err = gateway.OpenPostgres(context.Background(), testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	s, server = realServer(t, p)
	defer s.Close()
	defer server.Close()
	out := request(t, server, "GET", "/accounts/durable-upload/registration", nil, 200)
	if out["status"] != "ready" || out["source"] != "upload" {
		t.Fatal("uploaded credential structure did not survive reconstruction")
	}
	request(t, server, "POST", "/accounts/durable-upload/check", nil, 409)
	request(t, server, "POST", "/accounts/register", input, 200)
	statuses := make(chan int, 2)
	for _, seed := range []byte{82, 83} {
		payload, _ := json.Marshal(registrationInput("racing-upload", structuralSession(t, seed)))
		go func() {
			response, e := server.Client().Post(server.URL+"/accounts/register", "application/json", bytes.NewReader(payload))
			if e != nil {
				statuses <- 0
				return
			}
			defer response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	first, second := <-statuses, <-statuses
	if first != 200 && second != 200 || first != 409 && second != 409 {
		t.Fatalf("competing payloads must select one immutable registration: %d %d", first, second)
	}
}
