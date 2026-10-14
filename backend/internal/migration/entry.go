package migration

import (
	"database/sql"
	"embed"
	"fmt"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
	"io/fs"
	"log/slog"
)

//go:embed sql/*.sql
var migrations embed.FS

func manager(dsn string) (*Manager, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	files, _ := fs.Sub(migrations, "sql")
	m, err := NewManager(db, files, slog.Default(), "application", "configured")
	if err != nil {
		db.Close()
	}
	return m, err
}
func Apply(dsn string) error {
	m, e := manager(dsn)
	if e != nil {
		return e
	}
	defer m.migrate.Close()
	e = m.Up()
	if e != nil && e != migrate.ErrNoChange {
		return e
	}
	for _, user := range []string{"admin", "viewer"} {
		digest, e := bcrypt.GenerateFromPassword([]byte(user), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		if _, e = m.db.Exec(`INSERT INTO users(username,password_hash,role) VALUES($1,$2,$1) ON CONFLICT DO NOTHING`, user, string(digest)); e != nil {
			return e
		}
	}
	return nil
}
func Check(dsn string) (uint, error) {
	m, e := manager(dsn)
	if e != nil {
		return 0, e
	}
	defer m.migrate.Close()
	s, e := m.GetStatus()
	if e != nil {
		return 0, e
	}
	if s.Dirty || s.CurrentVersion != s.LatestVersion {
		return 0, fmt.Errorf("schema is stale or dirty: current=%d required=%d; run cmd/migrate", s.CurrentVersion, s.LatestVersion)
	}
	return s.CurrentVersion, nil
}
