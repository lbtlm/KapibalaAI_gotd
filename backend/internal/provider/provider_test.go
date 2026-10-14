package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/migration"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func database(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("real PostgreSQL required")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "provider_" + platform.ID()
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	dsn = u.String()
	if e = migration.Apply(dsn); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); p.Close() })
	return dsn
}
func post(t *testing.T, s *httptest.Server, path string, body any) (int, domain.TurnResponse) {
	t.Helper()
	b, _ := json.Marshal(body)
	r, e := s.Client().Post(s.URL+path, "application/json", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var out domain.TurnResponse
	json.NewDecoder(r.Body).Decode(&out)
	return r.StatusCode, out
}
func TestChatProviderPersistsReasoningAcrossRestartAndValidatesTools(t *testing.T) {
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Error(r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fake-key" {
			t.Error("missing provider key")
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		calls++
		tools := req["tools"].([]any)
		if len(tools) != 4 || req["parallel_tool_calls"] != false {
			t.Fatal("tool contract")
		}
		if calls == 1 {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": nil, "reasoning_content": "keep this reasoning", "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "get_recent_messages", "arguments": `{"limit":10}`}}}}}}})
		} else {
			messages := req["messages"].([]any)
			found := false
			for _, m := range messages {
				v := m.(map[string]any)
				if v["role"] == "assistant" && v["reasoning_content"] == "keep this reasoning" {
					found = true
				}
			}
			if !found {
				t.Error("reasoning lost on restart")
			}
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "done"}}}})
		}
	}))
	defer model.Close()
	cfg := provider.Config{DatabaseURL: database(t), Mode: "openai", APIStyle: "chat_completions", BaseURL: model.URL, APIKey: "fake-key", Model: "test"}
	p, e := provider.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	s := httptest.NewServer(p.Handler())
	req := domain.TurnRequest{RunID: "run-1", Tools: domain.Tools(), Messages: []domain.ChatMessage{{Role: "user", Content: []domain.Block{{Type: "text", Text: `{"groupId":"group"}`}}}}}
	st, out := post(t, s, "/agent/turn", req)
	if st != 200 || out.StopReason != "tool_use" {
		t.Fatalf("turn %d %v", st, out)
	}
	s.Close()
	p.Close()
	p, e = provider.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	s = httptest.NewServer(p.Handler())
	defer s.Close()
	req.Messages = append(req.Messages, domain.ChatMessage{Role: "assistant", Content: out.Content}, domain.ChatMessage{Role: "user", Content: []domain.Block{{Type: "tool_result", ToolUseID: "call-1", Content: `{"messages":[],"truncated":false}`}}})
	st, out = post(t, s, "/agent/turn", req)
	if st != 200 || out.StopReason != "end_turn" || out.Content[0].Text != "done" {
		t.Fatalf("resume %d %v", st, out)
	}
	req.Tools = req.Tools[:3]
	st, _ = post(t, s, "/agent/turn", req)
	if st != 400 {
		t.Fatalf("invalid tools %d", st)
	}
}
