package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider"
)

func TestSequenceGenerationUsesModelAndValidatesDraft(t *testing.T) {
	for _, style := range []string{"chat_completions", "responses"} {
		t.Run(style, func(t *testing.T) {
			draft := `{"name":"Reading club","steps":[{"index":1,"accountRole":"admin","text":"Welcome","delaySeconds":0},{"index":2,"accountRole":"member","text":"When?","delaySeconds":5}]}`
			calls := 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload map[string]any
				json.NewDecoder(r.Body).Decode(&payload)
				if payload["tools"] != nil || payload["model"] != "test-model" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("generation must use configured model without tools")
				}
				if style == "responses" {
					json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": draft}}}}})
				} else {
					json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": draft}}}})
				}
			}))
			defer model.Close()
			service, e := provider.New(context.Background(), provider.Config{DatabaseURL: database(t), Mode: "openai", APIStyle: style, APIKey: "test-key", Model: "test-model", BaseURL: model.URL})
			if e != nil {
				t.Fatal(e)
			}
			defer service.Close()
			request := func(body string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				service.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/sequences/generate", strings.NewReader(body)))
				return w
			}
			w := request(`{"prompt":"Reading club test conversation","stepCount":2}`)
			if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"model":"test-model"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"sequence"`)) {
				t.Fatalf("generation: %d %s", w.Code, w.Body)
			}
			draft = `{"name":"bad","steps":[{"index":1,"accountRole":"creator","text":"bad","delaySeconds":0}]}`
			w = request(`{"prompt":"test","stepCount":2}`)
			if w.Code != 502 || !strings.Contains(w.Body.String(), "AI_INVALID_SCRIPT") {
				t.Fatalf("invalid model output: %d %s", w.Code, w.Body)
			}
			before := calls
			for _, body := range []string{`{"prompt":"","stepCount":2}`, `{"prompt":"test","stepCount":101}`} {
				if w = request(body); w.Code != 400 {
					t.Fatalf("invalid request: %d", w.Code)
				}
			}
			if calls != before {
				t.Fatal("invalid input reached provider")
			}
		})
	}
}

func TestSequenceGenerationNeverFallsBackToScriptedOutput(t *testing.T) {
	service, e := provider.New(context.Background(), provider.Config{DatabaseURL: database(t), Mode: "scripted"})
	if e != nil {
		t.Fatal(e)
	}
	defer service.Close()
	w := httptest.NewRecorder()
	service.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/sequences/generate", strings.NewReader(`{"prompt":"test","stepCount":2}`)))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "AI_NOT_CONFIGURED") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
