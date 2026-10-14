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
	mode := os.Getenv("GATEWAY_MODE")
	if mode == "" {
		mode = "fake"
	}
	cfg := gateway.Config{Mode: mode, Store: store, AdminEnabled: os.Getenv("GATEWAY_ADMIN_ENABLED") == "true"}
	if mode == "telegram" {
		configs, e := gateway.LoadAccountConfigs(os.Getenv("TELEGRAM_ACCOUNTS_FILE"))
		if e != nil {
			slog.Error("read private Telegram account manifest")
			store.Close()
			os.Exit(1)
		}
		adapter, e := gateway.NewTelegramAdapter(ctx, store.DB, configs)
		if e != nil {
			slog.Error("configure Telegram adapter", "error", e)
			store.Close()
			os.Exit(1)
		}
		cfg.Telegram = adapter
	}
	service, e := gateway.New(ctx, cfg)
	if e != nil {
		slog.Error("start gateway", "error", e)
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
	slog.Info("gateway listening", "port", port, "mode", mode)
	if e = server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		slog.Error("gateway HTTP server", "error", e)
		os.Exit(1)
	}
}
