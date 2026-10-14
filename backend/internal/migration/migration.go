// Package migration provides database migration status tracking and management utilities.
package migration

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Status represents the current migration status of the database.
type Status struct {
	CurrentVersion      uint     `json:"current_version"`
	Dirty               bool     `json:"dirty"`
	AvailableMigrations int      `json:"available_migrations"`
	PendingCount        int      `json:"pending_count"`
	LatestVersion       uint     `json:"latest_version"`
	AppliedMigrations   []string `json:"applied_migrations,omitempty"`
	PendingMigrations   []string `json:"pending_migrations,omitempty"`
	DatabaseName        string   `json:"database_name"`
	DatabaseHost        string   `json:"database_host"`
}

// Manager handles database migrations with enhanced status tracking.
type Manager struct {
	db       *sql.DB
	fs       fs.FS
	log      *slog.Logger
	dbName   string
	dbHost   string
	migrate  *migrate.Migrate
	versions []uint
}

// NewManager creates a new migration manager.
func NewManager(db *sql.DB, migrationFS fs.FS, log *slog.Logger, dbName, dbHost string) (*Manager, error) {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return nil, fmt.Errorf("create migrate driver: %w", err)
	}

	source, err := iofs.New(migrationFS, ".")
	if err != nil {
		return nil, fmt.Errorf("create iofs source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return nil, fmt.Errorf("create migrate instance: %w", err)
	}

	mgr := &Manager{
		db:      db,
		fs:      migrationFS,
		log:     log,
		dbName:  dbName,
		dbHost:  dbHost,
		migrate: m,
	}

	if err := mgr.loadVersions(); err != nil {
		return nil, fmt.Errorf("load migration versions: %w", err)
	}

	return mgr, nil
}

// loadVersions scans the migration filesystem to find all available versions.
func (m *Manager) loadVersions() error {
	versionSet := make(map[uint]bool)
	versionRegex := regexp.MustCompile(`^(\d+)_.*\.(up|down)\.sql$`)

	entries, err := fs.ReadDir(m.fs, ".")
	if err != nil {
		return fmt.Errorf("read migration directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := versionRegex.FindStringSubmatch(entry.Name())
		if len(matches) >= 2 {
			v, err := strconv.ParseUint(matches[1], 10, 32)
			if err == nil {
				versionSet[uint(v)] = true
			}
		}
	}

	m.versions = make([]uint, 0, len(versionSet))
	for v := range versionSet {
		m.versions = append(m.versions, v)
	}
	sort.Slice(m.versions, func(i, j int) bool { return m.versions[i] < m.versions[j] })

	return nil
}

// GetStatus returns the current migration status.
func (m *Manager) GetStatus() (*Status, error) {
	currentVersion, dirty, err := m.migrate.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return nil, fmt.Errorf("get current version: %w", err)
	}

	var latestVersion uint
	if len(m.versions) > 0 {
		latestVersion = m.versions[len(m.versions)-1]
	}

	applied := make([]string, 0)
	pending := make([]string, 0)

	for _, v := range m.versions {
		name := m.getVersionName(v)
		if v <= currentVersion {
			applied = append(applied, name)
		} else {
			pending = append(pending, name)
		}
	}

	return &Status{
		CurrentVersion:      currentVersion,
		Dirty:               dirty,
		AvailableMigrations: len(m.versions),
		PendingCount:        len(pending),
		LatestVersion:       latestVersion,
		AppliedMigrations:   applied,
		PendingMigrations:   pending,
		DatabaseName:        m.dbName,
		DatabaseHost:        m.dbHost,
	}, nil
}

// getVersionName returns the migration file name for a given version.
func (m *Manager) getVersionName(version uint) string {
	entries, err := fs.ReadDir(m.fs, ".")
	if err != nil {
		return fmt.Sprintf("%03d", version)
	}

	prefix := fmt.Sprintf("%03d_", version)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), ".up.sql") {
			return strings.TrimSuffix(entry.Name(), ".up.sql")
		}
	}
	return fmt.Sprintf("%03d", version)
}

// Up runs all pending migrations.
func (m *Manager) Up() error {
	return m.migrate.Up()
}

// UpWithProgress runs all pending migrations with detailed logging.
func (m *Manager) UpWithProgress() (*MigrationResult, error) {
	beforeVersion, dirty, _ := m.migrate.Version()
	if dirty {
		return nil, fmt.Errorf("database is in dirty state at version %d, manual intervention required", beforeVersion)
	}

	err := m.migrate.Up()
	if err != nil && err != migrate.ErrNoChange {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	afterVersion, _, _ := m.migrate.Version()

	result := &MigrationResult{
		BeforeVersion:   beforeVersion,
		AfterVersion:    afterVersion,
		MigrationsRun:   int(afterVersion - beforeVersion),
		NoChange:        err == migrate.ErrNoChange,
		AppliedVersions: make([]string, 0),
	}

	if !result.NoChange {
		for _, v := range m.versions {
			if v > beforeVersion && v <= afterVersion {
				result.AppliedVersions = append(result.AppliedVersions, m.getVersionName(v))
			}
		}
	}

	m.logResult(result)
	return result, nil
}

// MigrationResult contains details about a migration run.
type MigrationResult struct {
	BeforeVersion   uint     `json:"before_version"`
	AfterVersion    uint     `json:"after_version"`
	MigrationsRun   int      `json:"migrations_run"`
	NoChange        bool     `json:"no_change"`
	AppliedVersions []string `json:"applied_versions,omitempty"`
}

// logResult logs the migration result with detailed information.
func (m *Manager) logResult(result *MigrationResult) {
	if result.NoChange {
		m.log.Info("database schema up to date",
			"version", result.AfterVersion,
			"database", m.dbName,
			"host", m.dbHost,
		)
		return
	}

	m.log.Info("database migrations applied",
		"from_version", result.BeforeVersion,
		"to_version", result.AfterVersion,
		"migrations_run", result.MigrationsRun,
		"applied", strings.Join(result.AppliedVersions, ", "),
		"database", m.dbName,
		"host", m.dbHost,
	)
}
