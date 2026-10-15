package domain

import "strings"

type AgentReplyInput struct {
	Context string `json:"context"`
	Message string `json:"message"`
}

func (v AgentReplyInput) Valid() bool {
	return strings.TrimSpace(v.Message) != "" && len([]rune(v.Message)) <= 2000 && len([]rune(v.Context)) <= 4000
}

type AgentReplyResult struct {
	Reply string `json:"reply"`
	Model string `json:"model"`
}
