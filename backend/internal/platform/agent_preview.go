package platform

import (
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"net/http"
	"strings"
)

func (a *App) agentStatus(w http.ResponseWriter, r *http.Request) error {
	var result struct {
		Mode       string `json:"mode"`
		Model      string `json:"model"`
		Configured bool   `json:"configured"`
	}
	if e := a.request(r.Context(), a.cfg.AgentURL, "GET", "/agent/status", nil, &result); e != nil {
		return fail(503, "AI_PROVIDER_ERROR")
	}
	respond(w, 200, result)
	return nil
}
func (a *App) previewAgentReply(w http.ResponseWriter, r *http.Request) error {
	var input domain.AgentReplyInput
	if e := decode(r, &input); e != nil {
		return e
	}
	if !input.Valid() {
		return fail(400, "VALIDATION_ERROR")
	}
	var result domain.AgentReplyResult
	if e := a.request(r.Context(), a.cfg.AgentURL, "POST", "/agent/reply-preview", input, &result); e != nil {
		return generationError(e)
	}
	if strings.TrimSpace(result.Reply) == "" || len([]rune(result.Reply)) > 2000 || result.Model == "" {
		return fail(502, "AI_INVALID_REPLY")
	}
	respond(w, 200, result)
	return nil
}
