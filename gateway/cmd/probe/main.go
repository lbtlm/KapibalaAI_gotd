package main

import (
	"context"
	"encoding/json"
	"flag"
	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
	"log/slog"
	"os"
)

func main() {
	join := flag.Bool("join", false, "one explicit join attempt per account to TELEGRAM_PROBE_GROUP; requires user authorization")
	accountID := flag.String("account", "", "probe only this configured alias")
	out := flag.String("out", "", "save private results to this local path instead of stdout")
	flag.Parse()
	ctx := context.Background()
	dsn, path := os.Getenv("DATABASE_URL"), os.Getenv("TELEGRAM_ACCOUNTS_FILE")
	if dsn == "" || path == "" {
		slog.Error("DATABASE_URL and TELEGRAM_ACCOUNTS_FILE required")
		os.Exit(1)
	}
	configs, e := gateway.LoadAccountConfigs(path)
	if e != nil {
		slog.Error("cannot read isolated account manifest")
		os.Exit(1)
	}
	if *accountID != "" {
		selected := make([]gateway.AccountConfig, 0, 1)
		for _, c := range configs {
			if c.ID == *accountID {
				selected = append(selected, c)
			}
		}
		if len(selected) == 0 {
			slog.Error("alias absent from manifest")
			os.Exit(1)
		}
		configs = selected
	}
	store, e := gateway.OpenPostgres(ctx, dsn)
	if e != nil {
		slog.Error("cannot open isolated gateway database", "error", e)
		os.Exit(1)
	}
	defer store.Close()
	username := os.Getenv("TELEGRAM_PROBE_GROUP")
	if username == "" {
		username = "test_kapibala"
	}
	var results []gateway.ProbeResult
	if *join {
		results = gateway.JoinTestGroup(ctx, store.DB, configs, username)
	} else {
		results = gateway.ProbeAccounts(ctx, store.DB, configs, username)
	}
	if *out != "" {
		b, e := json.MarshalIndent(results, "", "  ")
		if e != nil || os.WriteFile(*out, b, 0600) != nil {
			slog.Error("write private probe result failed")
			os.Exit(1)
		}
		slog.Info("private probe result saved", "account", *accountID)
	} else {
		_ = json.NewEncoder(os.Stdout).Encode(results)
	}
}
