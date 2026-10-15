package main

import (
	"context"
	"errors"
	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	mode := os.Getenv("GATEWAY_MODE")
	if mode != "" && mode != "telegram" {
		slog.Error("GATEWAY_MODE must be telegram; simulation is no longer supported")
		os.Exit(1)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("DATABASE_URL required")
		os.Exit(1)
	}
	store, e := gateway.OpenPostgres(ctx, dsn)
	if e != nil {
		slog.Error("open gateway store", "error", e)
		os.Exit(1)
	}
	adapter, e := gateway.NewTelegramAdapter(ctx, store.DB)
	if e != nil {
		slog.Error("configure Telegram adapter")
		store.Close()
		os.Exit(1)
	}
	cfg := gateway.Config{Store: store, Telegram: adapter}
	service, e := gateway.New(ctx, cfg)
	if e != nil {
		slog.Error("start gateway", "error", e)
		adapter.Close()
		store.Close()
		os.Exit(1)
	}
	defer service.Close()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	host := os.Getenv("BIND_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	server := &http.Server{Addr: host + ":" + port, Handler: service, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		select {
		case <-ctx.Done():
		case <-service.Done():
		}
		shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("gateway listening", "port", port, "mode", "telegram")
	if e = server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		slog.Error("gateway HTTP server", "error", e)
		os.Exit(1)
	}
}
