package gateway_test

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
)

func TestManifestAuthorizationCannotBeImportedUnderAnotherAlias(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	for _, window := range []string{"before-first-connect", "persisted-session", "persisted-session-source-missing", "persisted-session-source-changed"} {
		t.Run(window, func(t *testing.T) {
			pg, err := gateway.OpenPostgres(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pg.DB.Exec("DELETE FROM gateway_state; DELETE FROM gateway_account_configs; DELETE FROM gateway_sessions"); err != nil {
				pg.Close()
				t.Fatal(err)
			}
			auth := syntheticTelethonBytes(t, 171)
			path := filepath.Join(t.TempDir(), "manifest-fixture.session")
			if err = os.WriteFile(path, auth, 0600); err != nil {
				pg.Close()
				t.Fatal(err)
			}
			if window != "before-first-connect" {
				converted, e := gateway.ImportSession(path, "TelethonSQLite", "")
				if e != nil {
					pg.Close()
					t.Fatal(e)
				}
				if _, e = pg.DB.Exec("INSERT INTO gateway_sessions(account_id,data) VALUES($1,$2)", "static-account", converted); e != nil {
					pg.Close()
					t.Fatal(e)
				}
			}
			if window == "persisted-session-source-missing" {
				if err = os.Remove(path); err != nil {
					pg.Close()
					t.Fatal(err)
				}
			}
			if window == "persisted-session-source-changed" {
				if err = os.WriteFile(path, syntheticTelethonBytes(t, 173), 0600); err != nil {
					pg.Close()
					t.Fatal(err)
				}
			}
			configs := []gateway.AccountConfig{{ID: "static-account", APIID: 123, APIHash: "0123456789abcdef0123456789abcdef", SessionPath: path, SessionFormat: "TelethonSQLite"}}
			adapter, err := gateway.NewTelegramAdapter(context.Background(), pg.DB, configs)
			if err != nil {
				pg.Close()
				t.Fatal(err)
			}
			s, err := gateway.New(context.Background(), gateway.Config{Mode: "telegram", Store: pg, Telegram: adapter})
			if err != nil {
				pg.Close()
				t.Fatal(err)
			}
			ts := httptest.NewServer(s)
			defer func() {
				ts.Close()
				s.Close()
				db, e := sql.Open("pgx", dsn)
				if e == nil {
					defer db.Close()
					_, _ = db.Exec("DELETE FROM gateway_state; DELETE FROM gateway_account_configs; DELETE FROM gateway_sessions")
				}
			}()
			for phase := 0; phase < 2; phase++ {
				call(t, ts, "POST", "/accounts/register", registrationBody("duplicate-static", auth), 409)
				call(t, ts, "POST", "/accounts/duplicate-static/check", nil, 404)
				call(t, ts, "POST", "/accounts/static-account/check", nil, 409)
				call(t, ts, "POST", "/accounts/register", registrationBody("different-account", syntheticTelethonBytes(t, 172)), 200)
				if phase == 0 {
					ts.Close()
					s.Close()
					pg, err = gateway.OpenPostgres(context.Background(), dsn)
					if err != nil {
						t.Fatal(err)
					}
					adapter, err = gateway.NewTelegramAdapter(context.Background(), pg.DB, configs)
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
				}
			}
		})
	}
}
