package gateway

import (
	"context"
	"github.com/gotd/td/tg"
	"github.com/lbtlm/KapibalaAI_gotd/gateway/internal/ops"
	"sort"
	"strconv"
	"time"
)

func (s *Service) runSendRecovery(a interface {
	RecoverEffects(context.Context, string) error
}) {
	defer s.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			s.mu.Lock()
			var ids []string
			for id, acct := range s.state.Accounts {
				if acct.Status == "online" {
					ids = append(ids, id)
				}
			}
			s.mu.Unlock()
			sort.Strings(ids)
			for _, id := range ids {
				if s.ctx.Err() != nil {
					return
				}
				_ = a.RecoverEffects(s.ctx, id)
			}
		}
	}
}

// RecoverEffects reissues only the SAME account/peer/random-ID Telegram operation.
// Telegram returns its original message for a completed random ID, including across
// MTProto sessions; an in-flight RANDOM_ID_DUPLICATE is retained for a later attempt.
func (a *TelegramAdapter) RecoverEffects(ctx context.Context, aid string) error {
	a.mu.Lock()
	if a.recovering == nil {
		a.recovering = map[string]bool{}
	}
	if a.recovering[aid] {
		a.mu.Unlock()
		return nil
	}
	a.recovering[aid] = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.recovering, aid); a.mu.Unlock() }()
	live, err := a.online(aid)
	if err != nil {
		return err
	}
	s := a.service
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if acct := s.state.Accounts[aid]; acct != nil && timeNow().Before(acct.RateUntil) {
		wait := int((time.Until(acct.RateUntil) + time.Second - 1) / time.Second)
		s.mu.Unlock()
		return &APIError{429, "RATE_LIMITED", wait}
	}
	var ids []string
	for id, e := range s.state.Effects {
		if e.AccountID == aid && e.RandomID != 0 && e.State != "sent" && e.State != "failed" {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		s.mu.Lock()
		e := s.state.Effects[id]
		if e == nil || e.State == "sent" || e.State == "failed" {
			s.mu.Unlock()
			continue
		}
		copyEffect := *e
		g := s.state.Groups[e.GroupID]
		if g == nil {
			s.mu.Unlock()
			continue
		}
		peer := &tg.InputPeerChannel{ChannelID: g.ChannelID, AccessHash: g.AccessHashes[aid]}
		opCtx, stop, guardErr := s.operationContext(ctx)
		s.mu.Unlock()
		if guardErr != nil {
			return guardErr
		}
		callCtx, cancel := context.WithTimeout(opCtx, 10*time.Second)
		upd, rpcErr := ops.SendMessage(callCtx, live.api, &tg.MessagesSendMessageRequest{Peer: peer, Message: copyEffect.Text, RandomID: copyEffect.RandomID})
		cancel()
		stop()
		s.mu.Lock()
		e = s.state.Effects[id]
		if e == nil || e.State == "sent" {
			s.mu.Unlock()
			continue
		}
		if guardCtx, finish, err := s.operationContext(ctx); err != nil {
			s.mu.Unlock()
			return err
		} else {
			_ = guardCtx
			finish()
		}
		if rpcErr != nil {
			e.State = "unknown"
			api := platformError(rpcErr)
			if api.Status == 429 {
				acct := s.state.Accounts[aid]
				acct.RetrySeconds = api.RetryAfterSeconds
				acct.RateUntil = timeNow().Add(time.Duration(acct.RetrySeconds) * time.Second)
			}
			if api.Status == 401 || api.Status == 403 {
				e.State = "failed"
				s.emit("message_failed", map[string]any{"groupId": e.GroupID, "clientMsgId": e.ClientID, "code": api.Code})
				if api.Code == "ACCOUNT_SUSPENDED" {
					s.terminal(aid, "suspended")
				}
				if api.Code == "SESSION_EXPIRED" {
					s.terminal(aid, "session_expired")
				}
			}
			err = s.save(ctx)
			s.mu.Unlock()
			if err != nil {
				return err
			}
			return api
		}
		e.State = "accepted"
		if short, ok := upd.(*tg.UpdateShortSentMessage); ok {
			a.finalizeEffect(e, strconv.Itoa(short.ID), time.Unix(int64(short.Date), 0).UTC())
		} else {
			a.handleUpdates(aid, upd)
		}
		err = s.save(ctx)
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
