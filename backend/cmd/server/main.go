package main

import (
	"context"
	"errors"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
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
	cfg := platform.Config{DatabaseURL: os.Getenv("DATABASE_URL"), GatewayURL: os.Getenv("GATEWAY_URL"), AgentURL: os.Getenv("AGENT_URL")}
	if cfg.DatabaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	a, e := platform.New(ctx, cfg)
	if e != nil {
		slog.Error("backend startup refused", "error", e)
		os.Exit(1)
	}
	defer a.Close()
	a.Start(ctx)
	host := os.Getenv("BIND_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	s := &http.Server{Addr: net.JoinHostPort(host, port), Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Shutdown(shutdown)
	}()
	slog.Info("backend listening", "address", s.Addr)
	if e = s.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		slog.Error("backend stopped", "error", e)
		os.Exit(1)
	}
}
