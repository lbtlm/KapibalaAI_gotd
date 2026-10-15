package provider_test

import (
	"context"
	"encoding/json"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReplyPreviewUsesRealModelBoundaryWithoutTools(t *testing.T) {
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var input map[string]any
		json.NewDecoder(r.Body).Decode(&input)
		if input["tools"] != nil {
			t.Error("preview exposed tools")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"reply":"周六下午三点开始，欢迎参加读书会。"}`}}}})
	}))
	defer model.Close()
	service, e := provider.New(context.Background(), provider.Config{DatabaseURL: database(t), Mode: "openai", APIKey: "test-key", Model: "test-model", BaseURL: model.URL})
	if e != nil {
		t.Fatal(e)
	}
	defer service.Close()
	w := httptest.NewRecorder()
	service.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/agent/reply-preview", strings.NewReader(`{"context":"读书会周六15点开始","message":"几点开始？"}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "周六下午三点") || calls != 1 {
		t.Fatalf("reply preview: %d %s calls=%d", w.Code, w.Body, calls)
	}
	w = httptest.NewRecorder()
	service.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/agent/reply-preview", strings.NewReader(`{"message":""}`)))
	if w.Code != 400 || calls != 1 {
		t.Fatalf("invalid input reached model: %d calls=%d", w.Code, calls)
	}
	w = httptest.NewRecorder()
	service.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/agent/status", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"openai"`) || strings.Contains(w.Body.String(), "test-key") {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
}

func TestReplyPreviewRejectsInvalidModelOutput(t *testing.T) {
	for _, raw := range []string{`{"choices":[{"finish_reason":"length","message":{"content":"{}"}}]}`, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"reply\":\"\"}"}}]}`} {
		model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(raw)) }))
		service, e := provider.New(context.Background(), provider.Config{DatabaseURL: database(t), Mode: "openai", APIKey: "test-key", Model: "test-model", BaseURL: model.URL})
		if e != nil {
			t.Fatal(e)
		}
		w := httptest.NewRecorder()
		service.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/agent/reply-preview", strings.NewReader(`{"message":"你好"}`)))
		service.Close()
		model.Close()
		if w.Code != 502 || !strings.Contains(w.Body.String(), "AI_INVALID_REPLY") {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
}

func TestReplyPreviewDoesNotFallBackWithoutRealModel(t *testing.T) {
	service, e := provider.New(context.Background(), provider.Config{DatabaseURL: database(t), Mode: "scripted"})
	if e != nil {
		t.Fatal(e)
	}
	defer service.Close()
	w := httptest.NewRecorder()
	service.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/agent/reply-preview", strings.NewReader(`{"message":"你好"}`)))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "AI_NOT_CONFIGURED") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
