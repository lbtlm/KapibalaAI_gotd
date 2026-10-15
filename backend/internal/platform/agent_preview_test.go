package platform_test

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentPreviewIsAdminOnlyAndHasNoMessagingEffects(t *testing.T) {
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"reply":"周六三点开始。"}`}}}})
	}))
	defer model.Close()
	upstream := httptest.NewUnstartedServer(nil)
	_, server, cfg := fixtureConfig(t, platform.Config{AgentURL: "http://" + upstream.Listener.Addr().String()})
	service, e := provider.New(context.Background(), provider.Config{DatabaseURL: cfg.DatabaseURL, Mode: "openai", APIKey: "test-key", BaseURL: model.URL, Model: "test-model"})
	if e != nil {
		t.Fatal(e)
	}
	defer service.Close()
	upstream.Config.Handler = service.Handler()
	upstream.Start()
	defer upstream.Close()
	viewer, _ := login(t, server, "viewer")
	admin, _ := login(t, server, "admin")
	input := `{"context":"周六三点读书会","message":"几点开始？"}`
	for _, tt := range []struct {
		token  string
		status int
	}{{"", 401}, {viewer, 403}} {
		status, _, _ := call(t, server, "POST", "/api/agent/reply-preview", tt.token, input)
		if status != tt.status {
			t.Fatalf("permissions %d", status)
		}
	}
	if calls != 0 {
		t.Fatal("unauthorized model call")
	}
	status, out, _ := call(t, server, "POST", "/api/agent/reply-preview", admin, input)
	if status != 200 || out["reply"] != "周六三点开始。" || calls != 1 {
		t.Fatalf("preview %d %#v calls %d", status, out, calls)
	}
	status, out, _ = call(t, server, "GET", "/api/agent/status", viewer, "")
	if status != 200 || out["mode"] != "openai" || out["model"] != "test-model" {
		t.Fatalf("status %d %#v", status, out)
	}
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	for _, table := range []string{"agent_runs", "messages", "agent_effects"} {
		var count int
		if e = pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 0 {
			t.Fatalf("preview changed %s: %d %v", table, count, e)
		}
	}
}
