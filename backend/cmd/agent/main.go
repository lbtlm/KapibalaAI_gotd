package main

import (
	"context"
	"errors"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cfg := provider.Config{DatabaseURL: os.Getenv("DATABASE_URL"), Mode: os.Getenv("AGENT_MODE"), APIKey: os.Getenv("OPENAI_API_KEY"), Model: os.Getenv("OPENAI_MODEL"), BaseURL: os.Getenv("OPENAI_BASE_URL"), APIStyle: os.Getenv("OPENAI_API_STYLE"), AdminEnabled: os.Getenv("AGENT_ADMIN_ENABLED") == "true"}
	p, e := provider.New(ctx, cfg)
	if e != nil {
		slog.Error("agent startup refused", "error", e)
		os.Exit(1)
	}
	defer p.Close()
	host := os.Getenv("BIND_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}
	s := &http.Server{Addr: net.JoinHostPort(host, port), Handler: p.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Shutdown(shutdown)
	}()
	slog.Info("agent listening", "address", s.Addr, "mode", cfg.Mode)
	if e = s.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		slog.Error("agent stopped", "error", e)
		os.Exit(1)
	}
}
