package platform_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepeatedMigrationCommandPreservesExistingDataAndSessions(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	token, _ := login(t, s, "admin")
	call(t, s, "POST", "/api/accounts/account-1/transition", token, `{"to":"suspended","expectedFrom":"idle"}`)
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, "go", "run", "./cmd/migrate")
		cmd.Dir = filepath.Join("..", "..")
		cmd.Env = append(os.Environ(), "DATABASE_URL="+cfg.DatabaseURL)
		out, e := cmd.CombinedOutput()
		cancel()
		if e != nil || !strings.Contains(string(out), "schema ready") {
			t.Fatalf("repeat migration %d failed: %v %s", i+1, e, out)
		}
	}
	req, _ := http.NewRequest("GET", s.URL+"/api/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := s.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var accounts []map[string]any
	json.NewDecoder(res.Body).Decode(&accounts)
	if res.StatusCode != 200 || len(accounts) != 3 || accountStatus(t, s, token, "account-1") != "suspended" {
		t.Fatalf("migration reset data, seed, or existing access session: %d %v", res.StatusCode, accounts)
	}
	login(t, s, "viewer")
}

func TestServerExecutableRefusesPreviousSchemaWithoutListening(t *testing.T) {
	a, _, cfg := fixtureConfig(t)
	a.Close()
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	down, e := os.ReadFile(filepath.Join("..", "migration", "sql", "006_account_workspace.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(context.Background(), string(down)+`; UPDATE schema_migrations SET version=5,dirty=false`); e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(t.TempDir(), "backend-startup.exe")
	build := exec.Command("go", "build", "-o", binary, "./cmd/server")
	build.Dir = filepath.Join("..", "..")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build startup executable: %v %s", e, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), "DATABASE_URL="+cfg.DatabaseURL, "PORT=0")
	out, e := cmd.CombinedOutput()
	if e == nil || ctx.Err() != nil || !strings.Contains(string(out), "backend startup refused") || !strings.Contains(string(out), "current=5 required=6") || strings.Contains(string(out), "backend listening") {
		t.Fatalf("stale server did not promptly refuse startup: %v %s", e, out)
	}
}
