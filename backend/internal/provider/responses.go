package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"strings"
)

func (s *Service) responsesTurn(ctx context.Context, req domain.TurnRequest) (string, map[string]json.RawMessage, error) {
	input := []any{}
	for _, message := range req.Messages {
		for _, b := range message.Content {
			switch b.Type {
			case "text":
				input = append(input, map[string]any{"role": message.Role, "content": b.Text})
			case "tool_use":
				var raw json.RawMessage
				if e := s.pool.QueryRow(ctx, `SELECT items FROM provider_tool_items WHERE run_id=$1 AND tool_id=$2`, req.RunID, b.ID).Scan(&raw); e != nil || len(raw) == 0 {
					return "", nil, fmt.Errorf("persisted Responses items missing")
				}
				var items []json.RawMessage
				if json.Unmarshal(raw, &items) != nil {
					return "", nil, fmt.Errorf("invalid persisted Responses items")
				}
				for _, item := range items {
					input = append(input, item)
				}
			case "tool_result":
				input = append(input, map[string]any{"type": "function_call_output", "call_id": b.ToolUseID, "output": b.Content})
			}
		}
	}
	tools := []any{}
	for _, t := range req.Tools {
		tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.InputSchema, "strict": true})
	}
	raw, e := s.model(ctx, "/responses", map[string]any{"model": s.cfg.Model, "instructions": instructions, "input": input, "tools": tools, "parallel_tool_calls": false, "store": false, "include": []string{"reasoning.encrypted_content"}})
	if e != nil {
		return "", nil, e
	}
	var res struct {
		Output []json.RawMessage `json:"output"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return "", nil, fmt.Errorf("invalid Responses output")
	}
	out := domain.TurnResponse{StopReason: "end_turn", Content: []domain.Block{}}
	saved := map[string]json.RawMessage{}
	var text strings.Builder
	items, _ := json.Marshal(res.Output)
	for _, rawItem := range res.Output {
		var item struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(rawItem, &item) != nil {
			return "", nil, fmt.Errorf("invalid Responses item")
		}
		if item.Type == "function_call" {
			if !json.Valid([]byte(item.Arguments)) {
				return "", nil, fmt.Errorf("invalid function arguments")
			}
			out.StopReason = "tool_use"
			out.Content = append(out.Content, domain.Block{Type: "tool_use", ID: item.CallID, Name: item.Name, Input: json.RawMessage(item.Arguments)})
			saved[item.CallID] = items
		}
		if item.Type == "message" {
			for _, c := range item.Content {
				if c.Type == "output_text" {
					text.WriteString(c.Text)
				}
			}
		}
	}
	if len(out.Content) == 0 {
		out.Content = append(out.Content, domain.Block{Type: "text", Text: text.String()})
	}
	b, e := json.Marshal(out)
	return string(b), saved, e
}
