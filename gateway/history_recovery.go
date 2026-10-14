package gateway

import (
	"context"
	"github.com/gotd/td/tg"
	"net/http"
	"time"
)

func (s *Service) recoveryHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state.Groups[r.PathValue("groupId")]
	if g == nil {
		fail(w, &APIError{404, "GROUP_NOT_FOUND", 0})
		return
	}
	respond(w, 200, map[string]any{"recovery": g.Recovery})
}
func (a *TelegramAdapter) recordSDKRange(aid string, u tg.UpdatesClass) {
	var list []tg.UpdateClass
	switch v := u.(type) {
	case *tg.Updates:
		list = v.Updates
	case *tg.UpdatesCombined:
		list = v.Updates
	case *tg.UpdateShort:
		list = []tg.UpdateClass{v.Update}
	}
	for _, item := range list {
		var message tg.MessageClass
		switch v := item.(type) {
		case *tg.UpdateNewChannelMessage:
			message = v.Message
		case *tg.UpdateNewMessage:
			message = v.Message
		}
		var peer tg.PeerClass
		switch m := message.(type) {
		case *tg.Message:
			peer = m.PeerID
		case *tg.MessageService:
			peer = m.PeerID
		}
		if p, ok := peer.(*tg.PeerChannel); ok {
			if g := a.service.state.Groups["tg-"+stringID(p.ChannelID)]; g != nil {
				if g.HistoryThrough == nil {
					g.HistoryThrough = map[string]int{}
				}
				if message.GetID() > g.HistoryThrough[aid] {
					g.HistoryThrough[aid] = message.GetID()
				}
			}
		}
	}
}
func (a *TelegramAdapter) noteChannelGap(aid string, cid int64) {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.state.Groups["tg-"+stringID(cid)]
	if g == nil {
		return
	}
	if g.Recovery != nil && g.Recovery.Status == "blocked" {
		return
	}
	if g.Recovery == nil || g.Recovery.AccountID != aid {
		g.Recovery = &HistoryRecovery{AccountID: aid}
	}
	g.Recovery.Status = "blocked"
	g.Recovery.Code = "CHANNEL_GAP_UNRECOVERED"
	g.Recovery.UpdatedAt = timeNow()
	if e := s.save(context.Background()); e != nil {
		s.cancel()
	}
}

func (a *TelegramAdapter) recoverChannelRange(parent context.Context, aid string, api *tg.Client, channel *tg.InputChannel, target int, verifiedOnly bool) error {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	s := a.service
	gid := "tg-" + stringID(channel.ChannelID)
	s.mu.Lock()
	g := s.state.Groups[gid]
	if g == nil {
		s.mu.Unlock()
		return nil
	}
	if g.HistoryThrough == nil {
		g.HistoryThrough = map[string]int{}
	}
	through := g.HistoryThrough[aid]
	if g.VerifiedHistoryThrough == nil {
		g.VerifiedHistoryThrough = map[string]int{}
	}
	if old := g.Recovery; old != nil && old.AccountID == aid && old.FromID == 1 && old.ThroughID > g.VerifiedHistoryThrough[aid] && old.ThroughID <= through {
		g.VerifiedHistoryThrough[aid] = old.ThroughID
	}
	if verifiedOnly {
		through = g.VerifiedHistoryThrough[aid]
	}
	if g.Recovery != nil && through >= target && g.Recovery.Status == "recovered_with_limits" {
		s.mu.Unlock()
		return nil
	}
	recovery := &HistoryRecovery{AccountID: aid, Status: "pending", FromID: through + 1, ThroughID: through, TargetID: target, UpdatedAt: timeNow()}
	if previous := g.Recovery; previous != nil && previous.AccountID == aid && previous.Status != "recovered_with_limits" && previous.ThroughID == through && previous.FromID > 0 {
		recovery.FromID = previous.FromID
		recovery.RecoveredMessages = previous.RecoveredMessages
		recovery.UnavailableIDs = append([]int(nil), previous.UnavailableIDs...)
	}
	g.Recovery = recovery
	if target-through > 10000 {
		g.Recovery.Status = "blocked"
		g.Recovery.Code = "CHANNEL_HISTORY_RANGE_TOO_LARGE"
		e := s.save(ctx)
		s.mu.Unlock()
		if e != nil {
			return e
		}
		return &APIError{503, "CHANNEL_HISTORY_RANGE_TOO_LARGE", 0}
	}
	if e := s.save(ctx); e != nil {
		s.mu.Unlock()
		return e
	}
	s.mu.Unlock()
	blocked := func(code string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if g := s.state.Groups[gid]; g != nil {
			g.Recovery = recovery
			recovery.Status = "blocked"
			recovery.Code = code
			recovery.UpdatedAt = timeNow()
			if e := s.save(context.Background()); e != nil {
				s.cancel()
				return e
			}
		}
		return &APIError{503, code, 0}
	}
	for start := through + 1; start <= target; start += 100 {
		end := start + 99
		if end > target {
			end = target
		}
		ids := make([]tg.InputMessageClass, 0, end-start+1)
		for id := start; id <= end; id++ {
			ids = append(ids, &tg.InputMessageID{ID: id})
		}
		opCtx, stop, e := s.operationContext(ctx)
		if e != nil {
			return e
		}
		response, e := api.ChannelsGetMessages(opCtx, &tg.ChannelsGetMessagesRequest{Channel: channel, ID: ids})
		stop()
		if e != nil {
			return blocked("CHANNEL_HISTORY_UNAVAILABLE")
		}
		var messages []tg.MessageClass
		switch v := response.(type) {
		case *tg.MessagesMessages:
			messages = v.Messages
		case *tg.MessagesMessagesSlice:
			messages = v.Messages
		case *tg.MessagesChannelMessages:
			messages = v.Messages
		default:
			return blocked("CHANNEL_HISTORY_INCOMPLETE")
		}
		found := map[int]tg.MessageClass{}
		for _, message := range messages {
			found[message.GetID()] = message
		}
		ordered := make([]tg.MessageClass, 0, len(ids))
		var unavailable []int
		for id := start; id <= end; id++ {
			message := found[id]
			if message == nil {
				return blocked("CHANNEL_HISTORY_INCOMPLETE")
			}
			switch m := message.(type) {
			case *tg.Message:
				if p, ok := m.PeerID.(*tg.PeerChannel); !ok || p.ChannelID != channel.ChannelID {
					return blocked("CHANNEL_HISTORY_INCOMPLETE")
				}
			case *tg.MessageService:
				if p, ok := m.PeerID.(*tg.PeerChannel); !ok || p.ChannelID != channel.ChannelID {
					return blocked("CHANNEL_HISTORY_INCOMPLETE")
				}
			case *tg.MessageEmpty:
				unavailable = append(unavailable, id)
			}
			ordered = append(ordered, message)
		}
		s.mu.Lock()
		g = s.state.Groups[gid]
		if g == nil {
			s.mu.Unlock()
			return &APIError{404, "GROUP_NOT_FOUND", 0}
		}
		count := 0
		for _, message := range ordered {
			if _, ok := message.(*tg.Message); ok && !s.state.SeenUpdates[gid+":"+stringID(int64(message.GetID()))] {
				count++
			}
		}
		a.handleUpdates(aid, &tg.Updates{Updates: updatesForMessages(ordered)})
		if end > g.HistoryThrough[aid] {
			g.HistoryThrough[aid] = end
		}
		if g.VerifiedHistoryThrough[aid] >= start-1 {
			g.VerifiedHistoryThrough[aid] = end
		}
		g.Recovery = recovery
		recovery.ThroughID = end
		recovery.RecoveredMessages += count
		recovery.UnavailableIDs = append(recovery.UnavailableIDs, unavailable...)
		recovery.UpdatedAt = timeNow()
		e = s.save(ctx)
		s.mu.Unlock()
		if e != nil {
			s.cancel()
			return e
		}
	}
	opCtx, stop, e := s.operationContext(ctx)
	if e != nil {
		return e
	}
	participants, _, e := a.participants(opCtx, channel, &onlineSession{api: api})
	stop()
	if e != nil {
		return blocked("CHANNEL_MEMBERSHIP_UNAVAILABLE")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g = s.state.Groups[gid]
	if g == nil {
		return &APIError{404, "GROUP_NOT_FOUND", 0}
	}
	a.applyMembers(gid, g, s, participants, true)
	g.Recovery = recovery
	recovery.Status = "recovered_with_limits"
	recovery.Code = "MEMBERSHIP_HISTORY_UNAVAILABLE"
	recovery.MembershipSnapshotConfirmed = true
	recovery.UpdatedAt = timeNow()
	return s.save(ctx)
}
