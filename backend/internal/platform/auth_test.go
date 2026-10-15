package platform_test

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/migration"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, configs ...platform.Config) (*platform.App, *httptest.Server) {
	a, s, _ := fixtureConfig(t, configs...)
	return a, s
}
func fixtureConfig(t *testing.T, configs ...platform.Config) (*platform.App, *httptest.Server, platform.Config) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("real PostgreSQL TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ReplaceAll(platform.ID(), "-", "")
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	scoped := u.String()
	if err = migration.Apply(scoped); err != nil {
		t.Fatal(err)
	}
	cfg := platform.Config{}
	if len(configs) > 0 {
		cfg = configs[0]
	}
	cfg.DatabaseURL = scoped
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = "http://127.0.0.1:0"
	} // No listener and no gateway substitute.
	a, err := platform.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(a.Handler())
	t.Cleanup(func() { s.Close(); a.Close(); pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); pool.Close() })
	return a, s, cfg
}
func call(t *testing.T, s *httptest.Server, method, path, token, body string) (int, map[string]any, *http.Response) {
	t.Helper()
	req, _ := http.NewRequest(method, s.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := s.Client()
	if client == nil {
		client = http.DefaultClient
	}
	r, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	v := map[string]any{}
	json.NewDecoder(r.Body).Decode(&v)
	return r.StatusCode, v, r
}
func login(t *testing.T, s *httptest.Server, user string) (string, *http.Cookie) {
	status, b, r := call(t, s, "POST", "/api/auth/login", "", `{"username":"`+user+`","password":"`+user+`"}`)
	if status != 200 {
		t.Fatalf("login %d %#v", status, b)
	}
	cookies := r.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatal("refresh must be HttpOnly")
	}
	if _, ok := b["refreshToken"]; ok {
		t.Fatal("refresh leaked in body")
	}
	return b["accessToken"].(string), cookies[0]
}
func refresh(t *testing.T, s *httptest.Server, c *http.Cookie) (int, string, *http.Cookie) {
	req, _ := http.NewRequest("POST", s.URL+"/api/auth/refresh", nil)
	req.AddCookie(c)
	r, e := s.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b := map[string]any{}
	json.NewDecoder(r.Body).Decode(&b)
	access, _ := b["accessToken"].(string)
	var cookie *http.Cookie
	if len(r.Cookies()) > 0 {
		cookie = r.Cookies()[0]
	}
	return r.StatusCode, access, cookie
}
func TestRefreshReplayRevokesEntireFamilyAndLogoutIsImmediate(t *testing.T) {
	_, s := fixture(t)
	first, old := login(t, s, "admin")
	status, next, newCookie := refresh(t, s, old)
	if status != 200 || next == first {
		t.Fatal("rotation failed")
	}
	status, _, _ = refresh(t, s, old)
	if status != 401 {
		t.Fatal("old token replay accepted")
	}
	status, _, _ = call(t, s, "GET", "/api/accounts", next, "")
	if status != 401 {
		t.Fatal("new access survived replay")
	}
	status, _, _ = refresh(t, s, newCookie)
	if status != 401 {
		t.Fatal("new refresh survived replay")
	}
	token, _ := login(t, s, "admin")
	status, _, _ = call(t, s, "POST", "/api/auth/logout", token, "")
	if status != 200 {
		t.Fatal("logout failed")
	}
	status, _, _ = call(t, s, "GET", "/api/accounts", token, "")
	if status != 401 {
		t.Fatal("access survived logout")
	}
}
func TestViewerCannotWriteAndHealthRemainsPublic(t *testing.T) {
	_, s := fixture(t)
	token, _ := login(t, s, "viewer")
	status, b, _ := call(t, s, "POST", "/api/accounts/account-1/connect", token, "{}")
	if status != 403 || b["error"].(map[string]any)["code"] != "FORBIDDEN" {
		t.Fatalf("write permitted %d %#v", status, b)
	}
	status, b, _ = call(t, s, "GET", "/api/health", "", "")
	if status != 200 || b["ok"] != true {
		t.Fatalf("health %d %#v", status, b)
	}
}
func TestViewerCanLogOutOwnSessionWithoutGainingWritePermission(t *testing.T) {
	_, s := fixture(t)
	token, _ := login(t, s, "viewer")
	st, _, _ := call(t, s, "POST", "/api/auth/logout", token, "{}")
	if st != 200 {
		t.Fatalf("viewer cannot log out: %d", st)
	}
	st, _, _ = call(t, s, "GET", "/api/accounts", token, "")
	if st != 401 {
		t.Fatalf("viewer access survived logout: %d", st)
	}
}
