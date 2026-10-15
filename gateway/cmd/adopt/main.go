// Command adopt adds durable configuration to explicitly selected legacy accounts.
// It owns the normal writer lease and never starts a Telegram client.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"os"
	"sort"
	"strings"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway/internal/gateway"
)

func main() {
	manifest := flag.String("manifest", "", "explicit private legacy manifest")
	accounts := flag.String("accounts", "", "comma-separated explicit account aliases to adopt")
	flag.Parse()
	if *manifest == "" || *accounts == "" || os.Getenv("DATABASE_URL") == "" {
		slog.Error("DATABASE_URL, -manifest and -accounts are required; stop the gateway and back up first")
		os.Exit(1)
	}
	configured, err := gateway.LoadAccountConfigs(*manifest)
	if err != nil {
		slog.Error("cannot read private legacy manifest")
		os.Exit(1)
	}
	selected := map[string]bool{}
	for _, id := range strings.Split(*accounts, ",") {
		id = strings.TrimSpace(id)
		if id == "" || selected[id] {
			slog.Error("explicit account selection must be nonempty and unique")
			os.Exit(1)
		}
		selected[id] = true
	}
	var configs []gateway.AccountConfig
	var ids []string
	for _, c := range configured {
		if selected[c.ID] {
			configs = append(configs, c)
			ids = append(ids, c.ID)
		}
	}
	if len(configs) != len(selected) {
		slog.Error("selected aliases must each occur exactly once in manifest")
		os.Exit(1)
	}
	ctx := context.Background()
	store, err := gateway.OpenPostgres(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("cannot acquire offline gateway store; ensure the gateway is stopped")
		os.Exit(1)
	}
	count, err := store.AdoptAccounts(ctx, configs)
	store.Close()
	if err != nil {
		code := "GATEWAY_UNAVAILABLE"
		var api *gateway.APIError
		if errors.As(err, &api) {
			code = api.Code
		}
		slog.Error("offline adopt refused; Session and event data were not changed", "code", code)
		os.Exit(1)
	}
	sort.Strings(ids)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"adopted": count, "existing": len(ids) - count, "accountIds": ids})
}
