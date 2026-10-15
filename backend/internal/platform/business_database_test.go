package platform_test

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Rewind all later migrations, not only a historical data patch under test.
func rewindBusinessSchema(t *testing.T, cfg platform.Config, target int) {
	t.Helper()
	p, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var current int
	if e = p.QueryRow(context.Background(), `SELECT version FROM schema_migrations`).Scan(&current); e != nil {
		t.Fatal(e)
	}
	files, e := filepath.Glob(filepath.Join("..", "migration", "sql", "*.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	for _, file := range files {
		version, _ := strconv.Atoi(strings.Split(filepath.Base(file), "_")[0])
		if version > target && version <= current {
			data, e := os.ReadFile(file)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = p.Exec(context.Background(), string(data)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if _, e = p.Exec(context.Background(), `UPDATE schema_migrations SET version=$1,dirty=false`, target); e != nil {
		t.Fatal(e)
	}
}

// These are explicit business-row inputs in an isolated real PostgreSQL schema.
// They provide no Session, gateway response, Telegram identity or platform effect.
func businessSQL(t *testing.T, cfg platform.Config, query string, args ...any) {
	t.Helper()
	p, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if _, e = p.Exec(context.Background(), query, args...); e != nil {
		t.Fatal(e)
	}
}
func insertAccountRows(t *testing.T, cfg platform.Config, ids ...string) {
	t.Helper()
	for _, id := range ids {
		businessSQL(t, cfg, `INSERT INTO accounts(id) VALUES($1)`, id)
	}
}
func insertBusinessGroup(t *testing.T, cfg platform.Config) string {
	t.Helper()
	for i, id := range []string{"account-1", "account-2", "account-3"} {
		businessSQL(t, cfg, `INSERT INTO accounts(id,status,platform_user_id) VALUES($1,'online',$2)`, id, "database-input-"+string(rune('a'+i)))
	}
	businessSQL(t, cfg, `INSERT INTO groups(id,gateway_group_id,creator_account_id) VALUES('database-group','unroutable-database-group','account-1')`)
	for i, id := range []string{"account-1", "account-2", "account-3"} {
		role := []string{"creator", "admin", "member"}[i]
		businessSQL(t, cfg, `INSERT INTO members(group_id,platform_user_id,account_id,role) SELECT 'database-group',platform_user_id,id,$2 FROM accounts WHERE id=$1`, id, role)
	}
	businessSQL(t, cfg, `UPDATE groups SET admin_account_id='account-2' WHERE id='database-group'`)
	return "database-group"
}
func insertAgentInput(t *testing.T, cfg platform.Config) string {
	t.Helper()
	group := insertBusinessGroup(t, cfg)
	businessSQL(t, cfg, `UPDATE groups SET agent_enabled=true WHERE id=$1`, group)
	businessSQL(t, cfg, `INSERT INTO messages(group_id,msg_id,sender_platform_user_id,is_own,text) VALUES($1,'database-input','database-author',false,'persisted business input')`, group)
	businessSQL(t, cfg, `INSERT INTO agent_pending(group_id,msg_id) VALUES($1,'database-input')`, group)
	return group
}
func eventually(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("behavior did not converge before timeout")
}
func runList(t *testing.T, s *httptest.Server, token, group string) []map[string]any {
	t.Helper()
	req, _ := http.NewRequest("GET", s.URL+"/api/groups/"+group+"/agent-runs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := s.Client()
	if client == nil {
		client = http.DefaultClient
	}
	r, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var list []map[string]any
	json.NewDecoder(r.Body).Decode(&list)
	return list
}
func accountStatus(t *testing.T, s *httptest.Server, token, id string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", s.URL+"/api/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := s.Client()
	if client == nil {
		client = http.DefaultClient
	}
	r, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var values []map[string]any
	json.NewDecoder(r.Body).Decode(&values)
	for _, v := range values {
		if v["id"] == id {
			return v["status"].(string)
		}
	}
	return ""
}
