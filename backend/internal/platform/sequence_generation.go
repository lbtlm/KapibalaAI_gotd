package platform

import (
	"errors"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"net/http"
)

// Generation returns an editable draft; saving and running remain explicit actions.
func (a *App) generateSequence(w http.ResponseWriter, r *http.Request) error {
	var input domain.SequenceGenerationInput
	if e := decode(r, &input); e != nil {
		return e
	}
	if !input.Valid() {
		return fail(400, "VALIDATION_ERROR")
	}
	var result domain.SequenceGenerationResult
	if e := a.request(r.Context(), a.cfg.AgentURL, "POST", "/sequences/generate", input, &result); e != nil {
		return generationError(e)
	}
	if !result.Sequence.Valid() || len(result.Sequence.Steps) != input.StepCount || result.Model == "" {
		return fail(502, "AI_INVALID_SCRIPT")
	}
	respond(w, 200, result)
	return nil
}

func generationError(e error) error {
	var external *externalError
	if errors.As(e, &external) {
		switch external.Code {
		case "AI_NOT_CONFIGURED":
			return fail(503, external.Code)
		case "AI_INVALID_SCRIPT", "AI_INVALID_REPLY":
			return fail(502, external.Code)
		case "AI_TIMEOUT", "NETWORK_TIMEOUT":
			return fail(504, "AI_TIMEOUT")
		}
	}
	return fail(502, "AI_PROVIDER_ERROR")
}
