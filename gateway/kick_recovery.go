package gateway

import "context"

func (s *Service) lookupKickKey(gid, aid string, in input) (bool, any, error) {
	if in.IdempotencyKey == "" {
		return false, nil, nil
	}
	old := s.state.Effects["kick:"+in.IdempotencyKey]
	if old == nil {
		return false, nil, nil
	}
	if old.GroupID != gid || old.AccountID != aid || old.TargetID != in.TargetID {
		return true, nil, &APIError{409, "IDEMPOTENCY_CONFLICT", 0}
	}
	switch old.State {
	case "completed":
		return true, map[string]bool{"kicked": true}, nil
	case "failed":
		return true, nil, &APIError{old.Status, old.Code, 0}
	default:
		return true, nil, &APIError{504, "NETWORK_TIMEOUT", 0}
	}
}
func (s *Service) prepareKick(ctx context.Context, gid, aid string, in input) error {
	if in.IdempotencyKey == "" {
		return nil
	}
	if in.TargetID == "" {
		return &APIError{400, "INVALID_INPUT", 0}
	}
	id := "kick:" + in.IdempotencyKey
	s.state.Effects[id] = &effect{ID: id, GroupID: gid, AccountID: aid, TargetID: in.TargetID, State: "unknown"}
	return s.save(ctx)
}
func (s *Service) finishKick(key string, err error) {
	if key == "" {
		return
	}
	old := s.state.Effects["kick:"+key]
	if old == nil {
		return
	}
	if err == nil {
		old.State = "completed"
		return
	}
	api := platformError(err)
	if api.Status >= 400 && api.Status < 500 && api.Status != 429 {
		old.State = "failed"
		old.Status = api.Status
		old.Code = api.Code
	}
}
