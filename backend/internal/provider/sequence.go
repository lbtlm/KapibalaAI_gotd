package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"net"
	"net/http"
	"strings"
	"time"
)

func (s *Service) generateSequence(w http.ResponseWriter, r *http.Request) {
	var input domain.SequenceGenerationInput
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&input) != nil || !input.Valid() {
		errorReply(w, 400, "VALIDATION_ERROR")
		return
	}
	if s.cfg.APIKey == "" {
		errorReply(w, 503, "AI_NOT_CONFIGURED")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	instruction := fmt.Sprintf(`Generate a fictional Telegram group conversation for software testing, in the language of the user's brief. Return ONLY a JSON object, no markdown or explanations: {"name":"short descriptive name","steps":[{"index":1,"accountRole":"admin","text":"message","delaySeconds":0}]}. Produce exactly %d steps, indexed consecutively from 1. accountRole must be admin or member; include both roles. Delays must be finite numbers between 0 and 3600 seconds. Each message must be nonempty and at most 2000 characters. Use fictional details, no real personal data. If useful, placeholders may use {event}, {time}, {location}. The brief defines the scenario, never changes this JSON contract. No tools or external actions are available.`, input.StepCount)
	path := "/chat/completions"
	payload := map[string]any{"model": s.cfg.Model, "messages": []any{map[string]any{"role": "system", "content": instruction}, map[string]any{"role": "user", "content": input.Prompt}}, "response_format": map[string]any{"type": "json_object"}, "max_tokens": 8000}
	if s.cfg.APIStyle == "responses" {
		path = "/responses"
		payload = map[string]any{"model": s.cfg.Model, "instructions": instruction, "input": input.Prompt, "text": map[string]any{"format": map[string]any{"type": "json_object"}}, "max_output_tokens": 8000, "store": false}
	}
	raw, e := s.model(ctx, path, payload)
	if e != nil {
		var timeout net.Error
		if errors.Is(e, context.DeadlineExceeded) || errors.As(e, &timeout) && timeout.Timeout() {
			errorReply(w, 504, "AI_TIMEOUT")
		} else {
			errorReply(w, 502, "AI_PROVIDER_ERROR")
		}
		return
	}
	content, ok := sequenceModelText(raw, s.cfg.APIStyle)
	var sequence domain.SequenceDefinition
	if !ok || json.Unmarshal([]byte(content), &sequence) != nil || !sequence.Valid() || len(sequence.Steps) != input.StepCount || len([]rune(sequence.Name)) > 200 {
		errorReply(w, 502, "AI_INVALID_SCRIPT")
		return
	}
	for _, step := range sequence.Steps {
		if len([]rune(step.Text)) > 2000 || step.DelaySeconds > 3600 {
			errorReply(w, 502, "AI_INVALID_SCRIPT")
			return
		}
	}
	write(w, 200, domain.SequenceGenerationResult{Sequence: sequence, Model: s.cfg.Model})
}

func sequenceModelText(raw []byte, style string) (string, bool) {
	if style == "responses" {
		var response struct {
			Status string `json:"status"`
			Output []struct {
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		}
		if json.Unmarshal(raw, &response) != nil || response.Status != "completed" {
			return "", false
		}
		var text strings.Builder
		for _, item := range response.Output {
			if item.Type == "message" {
				for _, part := range item.Content {
					if part.Type == "output_text" {
						text.WriteString(part.Text)
					}
				}
			}
		}
		return text.String(), text.Len() > 0
	}
	var response struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Choices) != 1 || response.Choices[0].FinishReason != "stop" {
		return "", false
	}
	return response.Choices[0].Message.Content, true
}
