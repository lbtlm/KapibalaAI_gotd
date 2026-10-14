package domain_test

import (
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"testing"
)

func TestVariablesPersistFromOverrideAndEmptyDoesNotErase(t *testing.T) {
	steps := []domain.SequenceStep{{Index: 1, Text: "{event} {time}"}, {Index: 2, Text: "{event} {location}"}, {Index: 3, Text: "{location}"}}
	got, err := domain.Resolve(steps, map[string]string{"event": "分享会", "time": "12:00"}, map[string]map[string]string{"2": {"location": "共享盘"}, "3": {"location": ""}})
	if err != nil {
		t.Fatal(err)
	}
	if got[2].Text != "共享盘" || got[2].VarSources["location"] != "step:2" {
		t.Fatalf("inherited override: %#v", got[2])
	}
}
func TestUnresolvedThirdStepRejectsWholeSequence(t *testing.T) {
	_, err := domain.Resolve([]domain.SequenceStep{{Index: 1, Text: "ok"}, {Index: 2, Text: "ok"}, {Index: 3, Text: "{missing}"}}, nil, nil)
	e, ok := err.(*domain.PlaceholderError)
	if !ok || e.StepIndex != 3 || e.Key != "missing" {
		t.Fatalf("expected third step missing, got %v", err)
	}
}
