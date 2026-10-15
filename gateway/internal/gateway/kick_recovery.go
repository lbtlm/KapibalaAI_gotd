package gateway

import "context"

func (s *Service) lookupMemberKey(op, gid, aid string, in input) (bool, any, error) {
	if in.IdempotencyKey == "" {
		return false, nil, nil
	}
	old := s.state.Effects[op+":"+in.IdempotencyKey]
	if old == nil {
		return false, nil, nil
	}
	target := in.TargetID
	if op == "promote" {
		target = in.AccountID
	}
	if old.GroupID != gid || old.AccountID != aid || old.TargetID != target {
		return true, nil, &APIError{409, "IDEMPOTENCY_CONFLICT", 0}
	}
	if err := s.accountError(aid, false); err != nil {
		return true, nil, err
	}
	switch old.State {
	case "completed":
		if op == "promote" {
			return true, map[string]any{}, nil
		}
		return true, map[string]bool{"kicked": true}, nil
	case "failed":
		return true, nil, &APIError{old.Status, old.Code, 0}
	default:
		if op == "promote" && s.state.Accounts[aid].Status == "online" {
			return false, nil, nil // Prepared calls can proceed; unknown calls only reconcile role.
		}
		return true, nil, &APIError{504, "NETWORK_TIMEOUT", 0}
	}
}
func (s *Service) prepareMemberEffect(ctx context.Context, op, gid, aid string, in input) error {
	if in.IdempotencyKey == "" {
		return nil
	}
	target, stage := in.TargetID, "unknown"
	if op == "promote" {
		target, stage = in.AccountID, "prepared"
	}
	if target == "" {
		return &APIError{400, "INVALID_INPUT", 0}
	}
	id := op + ":" + in.IdempotencyKey
	if s.state.Effects[id] != nil {
		return nil
	}
	s.state.Effects[id] = &effect{ID: id, GroupID: gid, AccountID: aid, TargetID: target, State: stage}
	return s.save(ctx)
}
func (s *Service) finishMemberEffect(op, key string, err error) {
	if key == "" {
		return
	}
	old := s.state.Effects[op+":"+key]
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
