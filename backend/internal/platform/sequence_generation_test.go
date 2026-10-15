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

func TestGeneratedSequenceIsAdminOnlyDraftUntilSaved(t *testing.T) {
	modelCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"name":"AI draft","steps":[{"index":1,"accountRole":"admin","text":"Welcome","delaySeconds":0},{"index":2,"accountRole":"member","text":"Question","delaySeconds":5}]}`}}}})
	}))
	defer model.Close()
	// Provider and backend share the real isolated schema, as in Compose.
	providerServer := httptest.NewUnstartedServer(nil)
	_, server, cfg := fixtureConfig(t, platform.Config{AgentURL: "http://" + providerServer.Listener.Addr().String()})
	service, e := provider.New(context.Background(), provider.Config{DatabaseURL: cfg.DatabaseURL, Mode: "openai", APIKey: "test-key", BaseURL: model.URL, Model: "test-model"})
	if e != nil {
		t.Fatal(e)
	}
	defer service.Close()
	providerServer.Config.Handler = service.Handler()
	providerServer.Start()
	defer providerServer.Close()
	viewer, _ := login(t, server, "viewer")
	admin, _ := login(t, server, "admin")
	input := `{"prompt":"Test welcome conversation","stepCount":2}`
	for _, tt := range []struct {
		token  string
		status int
	}{{"", 401}, {viewer, 403}} {
		status, _, _ := call(t, server, "POST", "/api/sequences/generate", tt.token, input)
		if status != tt.status {
			t.Fatalf("permissions %d", status)
		}
	}
	if modelCalls != 0 {
		t.Fatal("unauthorized generation reached model")
	}
	status, out, _ := call(t, server, "POST", "/api/sequences/generate", admin, input)
	if status != 200 || out["model"] != "test-model" {
		t.Fatalf("generate %d %#v", status, out)
	}
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	var count int
	if e = pool.QueryRow(context.Background(), `SELECT count(*) FROM sequences`).Scan(&count); e != nil || count != 0 {
		t.Fatal("generation must not save", e, count)
	}
	body, _ := json.Marshal(out["sequence"])
	if status, _, _ = call(t, server, "POST", "/api/sequences", admin, string(body)); status != 201 {
		t.Fatalf("save generated draft %d", status)
	}
	if modelCalls != 1 {
		t.Fatal("unexpected provider retry", modelCalls)
	}
}
