package gateway

import (
	"errors"
	"io"
	"net/http"
)

func (s *Service) groupHTTP(w http.ResponseWriter, r *http.Request) {
	var in input
	if r.Method == "POST" {
		if err := decode(r, &in); err != nil && !(r.PathValue("operation") == "invite" && errors.Is(err, io.EOF)) {
			fail(w, &APIError{400, "INVALID_INPUT", 0})
			return
		}
	}
	gid, op := r.PathValue("groupId"), r.PathValue("operation")
	if r.Method == "GET" {
		op = "members"
	}
	if gid == "" {
		op = "create"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	opCtx, stop, err := s.operationContext(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	defer stop()
	aid := in.AccountID
	if in.ByAccountID != "" {
		aid = in.ByAccountID
	}
	if op == "create" {
		aid = in.CreatorAccountID
	}
	if op == "kick" || op == "promote" {
		if handled, result, e := s.lookupMemberKey(op, gid, aid, in); handled {
			if e != nil {
				fail(w, e)
			} else {
				respond(w, 200, result)
			}
			return
		}
	}
	if op != "members" && op != "invite" {
		if err := s.accountError(aid, true); err != nil {
			fail(w, err)
			return
		}
	}
	g := s.state.Groups[gid]
	if op != "create" && g == nil {
		fail(w, &APIError{404, "GROUP_NOT_FOUND", 0})
		return
	}
	if op == "kick" || op == "promote" {
		if e := s.prepareMemberEffect(opCtx, op, gid, aid, in); e != nil {
			fail(w, e)
			return
		}
	}
	out, err := s.cfg.Telegram.Execute(opCtx, op, gid, in, s)
	if op == "kick" || op == "promote" {
		s.finishMemberEffect(op, in.IdempotencyKey, err)
	}
	status := 200
	if op == "join" || op == "send" {
		status = 202
	}
	s.commitHTTP(w, r, status, out, err)
	return
}
