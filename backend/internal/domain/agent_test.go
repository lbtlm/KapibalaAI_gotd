package domain_test

import (
	"encoding/json"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"testing"
)

func TestFinalTextMustBePresentAndMayBeEmpty(t *testing.T) {
	if _, e := domain.ParseTurn([]byte(`{"stop_reason":"end_turn","content":[{"type":"text"}]}`)); e == nil {
		t.Fatal("missing text accepted")
	}
	if _, e := domain.ParseTurn([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":""}]}`)); e != nil {
		t.Fatalf("empty but present text rejected: %v", e)
	}
	raw, _ := json.Marshal(domain.TurnResponse{StopReason: "end_turn", Content: []domain.Block{{Type: "text", Text: ""}}})
	if _, e := domain.ParseTurn(raw); e != nil {
		t.Fatalf("empty text was lost in serialization: %s", raw)
	}
}
