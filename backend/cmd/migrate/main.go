package main

import (
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/migration"
	"log/slog"
	"os"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	if e := migration.Apply(dsn); e != nil {
		slog.Error("migration failed", "error", e)
		os.Exit(1)
	}
	version, e := migration.Check(dsn)
	if e != nil {
		slog.Error("schema check failed", "error", e)
		os.Exit(1)
	}
	slog.Info("schema ready", "version", version)
}
