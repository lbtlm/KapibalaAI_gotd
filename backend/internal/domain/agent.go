package domain

import (
	"encoding/json"
	"fmt"
	"math"
)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}
type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

func (b Block) MarshalJSON() ([]byte, error) {
	if b.Type == "text" {
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{b.Type, b.Text})
	}
	type plain Block
	return json.Marshal(plain(b))
}

type ChatMessage struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}
type TurnRequest struct {
	RunID    string        `json:"runId"`
	Tools    []Tool        `json:"tools"`
	Messages []ChatMessage `json:"messages"`
}
type TurnResponse struct {
	StopReason string  `json:"stop_reason"`
	Content    []Block `json:"content"`
}

func Tools() []Tool {
	return []Tool{
		tool("get_recent_messages", "Read recent group messages", map[string]any{"limit": map[string]any{"type": "number"}}),
		tool("send_message", "Send an audited message", map[string]any{"text": map[string]any{"type": "string"}, "idempotency_key": map[string]any{"type": "string"}}),
		tool("kick_user", "Remove a group member if policy allows", map[string]any{"platform_user_id": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}}),
		tool("finish", "Finish this run", map[string]any{"summary": map[string]any{"type": "string"}}),
	}
}
func tool(name, description string, properties map[string]any) Tool {
	required := []string{}
	for _, key := range []string{"limit", "text", "idempotency_key", "platform_user_id", "reason", "summary"} {
		if _, ok := properties[key]; ok {
			required = append(required, key)
		}
	}
	return Tool{name, description, map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
}
func ParseTurn(raw []byte) (TurnResponse, error) {
	var r TurnResponse
	if json.Unmarshal(raw, &r) != nil || len(r.Content) != 1 {
		return r, fmt.Errorf("BAD_JSON")
	}
	b := r.Content[0]
	if r.StopReason == "end_turn" && b.Type == "text" {
		var shape struct {
			Content []map[string]json.RawMessage `json:"content"`
		}
		json.Unmarshal(raw, &shape)
		value, ok := shape.Content[0]["text"]
		if !ok || len(value) == 0 || value[0] != '"' {
			return r, fmt.Errorf("BAD_JSON")
		}
		return r, nil
	}
	if r.StopReason == "tool_use" && b.Type == "tool_use" && b.ID != "" && b.Name != "" && len(b.Input) > 0 {
		return r, nil
	}
	return r, fmt.Errorf("BAD_JSON")
}
func ValidateInput(name string, input json.RawMessage) string {
	var value map[string]any
	if json.Unmarshal(input, &value) != nil || value == nil {
		return "INVALID_INPUT"
	}
	var found *Tool
	for _, t := range Tools() {
		if t.Name == name {
			tt := t
			found = &tt
		}
	}
	if found == nil {
		return "UNKNOWN_TOOL"
	}
	properties := found.InputSchema["properties"].(map[string]any)
	if len(value) != len(properties) {
		return "INVALID_INPUT"
	}
	for key, schema := range properties {
		v, ok := value[key]
		if !ok {
			return "INVALID_INPUT"
		}
		kind := schema.(map[string]any)["type"]
		if kind == "string" {
			s, ok := v.(string)
			if !ok || (s == "" && key != "summary") {
				return "INVALID_INPUT"
			}
		} else {
			n, ok := v.(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
				return "INVALID_INPUT"
			}
		}
	}
	return ""
}
