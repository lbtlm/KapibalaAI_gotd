package gateway_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
)

func syntheticTelethonBytes(t *testing.T, seed byte) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.session")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE sessions (dc_id INTEGER, server_address TEXT, port INTEGER, auth_key BLOB)`); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 256)
	for i := range key {
		key[i] = seed
	}
	if _, err = db.Exec(`INSERT INTO sessions VALUES (2,'149.154.167.51',443,?)`, key); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestConnectionIncarnationCannotBeRevivedOrOverwrittenByOldOfflineEvent(t *testing.T) {
	store := gateway.NewMemoryStore()
	s, err := gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store, AdminEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	call(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "old"}, 200)
	call(t, ts, "POST", "/admin/events", map[string]any{"type": "account_status", "data": map[string]string{"accountId": "account-1", "status": "offline", "connectionId": "old"}}, 200)
	call(t, ts, "POST", "/accounts/account-1/check", nil, 409)
	call(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "old"}, 409)
	ts.Close()
	s.Close()
	s, err = gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store, AdminEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ts = httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events?since=0", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(res.Body)
	found := false
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "data: ") {
			var e map[string]any
			_ = json.Unmarshal([]byte(line[6:]), &e)
			if e["status"] == "offline" && e["connectionId"] == "old" {
				found = true
				break
			}
		}
	}
	res.Body.Close()
	if !found {
		t.Fatal("offline generation was not replayed after restart")
	}
	call(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "new"}, 200)
	call(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "new"}, 200)
	call(t, ts, "POST", "/admin/events", map[string]any{"type": "account_status", "data": map[string]string{"accountId": "account-1", "status": "offline", "connectionId": "old"}}, 200)
	profile := call(t, ts, "POST", "/accounts/account-1/check", nil, 200)
	if profile["source"] != "simulated" {
		t.Fatal("fake identity claimed platform verification", profile)
	}
	call(t, ts, "POST", "/accounts/account-1/disconnect", nil, 200)
	call(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "new"}, 409)
}
func registrationBody(id string, b []byte) map[string]any {
	return map[string]any{"accountId": id, "session": base64.StdEncoding.EncodeToString(b), "apiId": 123, "apiHash": "0123456789abcdef0123456789abcdef"}
}
func TestAccountRegistrationSurvivesRestartWithoutSourceAndRejectsOverwrite(t *testing.T) {
	store := gateway.NewMemoryStore()
	s, err := gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	input := registrationBody("import-1", syntheticTelethonBytes(t, 11))
	call(t, ts, "POST", "/accounts/register", input, 200)
	call(t, ts, "POST", "/accounts/import-1/check", nil, 409)
	call(t, ts, "POST", "/accounts/register", input, 200)
	changed := registrationBody("import-1", syntheticTelethonBytes(t, 12))
	call(t, ts, "POST", "/accounts/register", changed, 409)
	alias := registrationBody("alias", syntheticTelethonBytes(t, 11))
	call(t, ts, "POST", "/accounts/register", alias, 409)
	ts.Close()
	s.Close()
	s, err = gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	ts = httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	call(t, ts, "POST", "/accounts/register", input, 200)
	out := call(t, ts, "POST", "/accounts/import-1/connect", map[string]string{"connectionId": "generation-1"}, 200)
	if out["connectionId"] != "generation-1" {
		t.Fatalf("missing connection identity: %v", out)
	}
	call(t, ts, "POST", "/accounts/import-1/check", nil, 200)
}

type registrationSaveLoss struct {
	*gateway.PostgresStore
	fail atomic.Bool
}

func (s *registrationSaveLoss) Save(ctx context.Context, b []byte) error {
	if s.fail.CompareAndSwap(true, false) {
		return errors.New("injected registration state commit failure")
	}
	return s.PostgresStore.Save(ctx, b)
}

func TestPostgresRegistrationReconstructsAfterConfigCommitBeforeHTTPAcknowledgement(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	pg, err := gateway.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	// This test owns only the isolated gateway database and resets fake snapshot
	// before switching that empty snapshot to the real adapter registration path.
	if _, err = pg.DB.Exec("DELETE FROM gateway_state; DELETE FROM gateway_account_configs; DELETE FROM gateway_sessions"); err != nil {
		pg.Close()
		t.Fatal(err)
	}
	adapter, err := gateway.NewTelegramAdapter(context.Background(), pg.DB, nil)
	if err != nil {
		pg.Close()
		t.Fatal(err)
	}
	store := &registrationSaveLoss{PostgresStore: pg}
	s, err := gateway.New(context.Background(), gateway.Config{Mode: "telegram", Store: store, Telegram: adapter})
	if err != nil {
		pg.Close()
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	input := registrationBody("persisted-import", syntheticTelethonBytes(t, 91))
	store.fail.Store(true)
	call(t, ts, "POST", "/accounts/register", input, 503)
	ts.Close()
	s.Close()
	pg, err = gateway.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err = gateway.NewTelegramAdapter(context.Background(), pg.DB, nil)
	if err != nil {
		pg.Close()
		t.Fatal(err)
	}
	s, err = gateway.New(context.Background(), gateway.Config{Mode: "telegram", Store: pg, Telegram: adapter})
	if err != nil {
		pg.Close()
		t.Fatal(err)
	}
	ts = httptest.NewServer(s)
	defer func() {
		ts.Close()
		s.Close()
		cleanup, e := sql.Open("pgx", dsn)
		if e == nil {
			defer cleanup.Close()
			_, _ = cleanup.Exec("DELETE FROM gateway_state; DELETE FROM gateway_account_configs; DELETE FROM gateway_sessions")
		}
	}()
	call(t, ts, "POST", "/accounts/persisted-import/check", nil, 409)
	call(t, ts, "POST", "/accounts/register", input, 200)
	conflict := registrationBody("persisted-import", syntheticTelethonBytes(t, 91))
	conflict["apiId"] = 456
	call(t, ts, "POST", "/accounts/register", conflict, 409)
	alias := registrationBody("duplicate-import", syntheticTelethonBytes(t, 91))
	call(t, ts, "POST", "/accounts/register", alias, 409)
	call(t, ts, "POST", "/accounts/duplicate-import/check", nil, 404)
	type attempt struct {
		status int
		body   map[string]any
	}
	results := make(chan attempt, 2)
	for _, seed := range []byte{92, 93} {
		request := registrationBody("concurrent-import", syntheticTelethonBytes(t, seed))
		go func() {
			b, _ := json.Marshal(request)
			response, e := http.Post(ts.URL+"/accounts/register", "application/json", bytes.NewReader(b))
			status := 0
			if e == nil {
				status = response.StatusCode
				response.Body.Close()
			}
			results <- attempt{status, request}
		}()
	}
	first, second := <-results, <-results
	if first.status != 200 && second.status != 200 || first.status != 409 && second.status != 409 {
		t.Fatalf("concurrent imports must choose one payload: %d %d", first.status, second.status)
	}
	for _, result := range []attempt{first, second} {
		call(t, ts, "POST", "/accounts/register", result.body, result.status)
	}
}

func TestPostgresOrdinaryOfflineEventAndEndedGenerationSurviveRestart(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	store, err := gateway.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store, AdminEnabled: true})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	call(t, ts, "POST", "/admin/reset", nil, 200)
	call(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "durable-old"}, 200)
	call(t, ts, "POST", "/admin/events", map[string]any{"type": "account_status", "data": map[string]string{"accountId": "account-1", "status": "offline", "connectionId": "durable-old"}}, 200)
	ts.Close()
	s.Close()
	store, err = gateway.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	s, err = gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	ts = httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	call(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "durable-old"}, 409)
	call(t, ts, "POST", "/accounts/account-1/check", nil, 409)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events?since=0", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	scan := bufio.NewScanner(res.Body)
	for scan.Scan() {
		if line := scan.Text(); strings.HasPrefix(line, "data: ") {
			var event map[string]any
			_ = json.Unmarshal([]byte(line[6:]), &event)
			if event["type"] == "account_status" && event["status"] == "offline" && event["connectionId"] == "durable-old" {
				return
			}
		}
	}
	t.Fatal("ordinary offline SSE was not durable across PostgreSQL reconstruction")
}
