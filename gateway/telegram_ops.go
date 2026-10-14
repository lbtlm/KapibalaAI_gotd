package gateway

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/gotd/td/tg"
	"github.com/lbtlm/KapibalaAI_gotd/gateway/internal/ops"
	"net/http"
	"sort"
	"strconv"
	"time"
)

func (s *Service) importHTTP(w http.ResponseWriter, r *http.Request) {
	var in input
	if decode(r, &in) != nil || in.Username == "" || len(in.AccountIDs) == 0 {
		fail(w, &APIError{400, "INVALID_INPUT", 0})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Telegram == nil {
		fail(w, &APIError{400, "TELEGRAM_MODE_REQUIRED", 0})
		return
	}
	for _, id := range in.AccountIDs {
		if e := s.accountError(id, true); e != nil {
			fail(w, e)
			return
		}
	}
	out, e := s.cfg.Telegram.Execute(r.Context(), "import", "", in, s)
	s.commitHTTP(w, r, 200, out, e)
}

func (a *TelegramAdapter) Execute(ctx context.Context, op, gid string, in input, s *Service) (any, error) {
	if op == "import" {
		return a.importGroup(ctx, in, s)
	}
	aid := in.AccountID
	if in.ByAccountID != "" {
		aid = in.ByAccountID
	}
	if op == "create" {
		aid = in.CreatorAccountID
	}
	g := s.state.Groups[gid]
	if aid == "" && g != nil {
		ids := make([]string, 0, len(g.AccessHashes))
		for id := range g.AccessHashes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if acct := s.state.Accounts[id]; acct != nil && acct.Status == "online" {
				if _, e := a.online(id); e == nil {
					aid = id
					break
				}
			}
		}
	}
	live, e := a.online(aid)
	if e != nil {
		return nil, e
	}
	if op == "create" {
		return a.createGroup(ctx, aid, in, s, live)
	}
	if op == "join" {
		pid := s.state.Accounts[aid].PlatformID
		if g.Members[pid] {
			return nil, &APIError{409, "ALREADY_MEMBER", 0}
		}
		upd, err := ops.ImportChatInvite(ctx, live.api, inviteHash(in.InviteLink))
		if err != nil {
			return nil, platformError(err)
		}
		for _, chat := range updateChats(upd) {
			if ch, ok := chat.(*tg.Channel); ok && ch.ID == g.ChannelID {
				g.AccessHashes[aid] = ch.AccessHash
			}
		}
		a.handleUpdates(aid, upd)
		if !g.Members[pid] {
			g.Members[pid] = true
			g.MemberRoles[pid] = "member"
			s.emit("member_joined", map[string]any{"groupId": gid, "platformUserId": pid})
		}
		return map[string]bool{"accepted": true}, nil
	}
	channel := &tg.InputChannel{ChannelID: g.ChannelID, AccessHash: g.AccessHashes[aid]}
	peer := &tg.InputPeerChannel{ChannelID: g.ChannelID, AccessHash: g.AccessHashes[aid]}
	if g.AccessHashes[aid] == 0 {
		return nil, &APIError{403, "SENDER_NOT_IN_GROUP", 0}
	}
	switch op {
	case "invite":
		link, err := ops.ExportChatInvite(ctx, live.api, &tg.MessagesExportChatInviteRequest{Peer: peer})
		if err != nil {
			return nil, platformError(err)
		}
		invite, ok := link.(*tg.ChatInviteExported)
		if !ok {
			return nil, &APIError{503, "GATEWAY_UNAVAILABLE", 0}
		}
		g.Invite = invite.Link
		return map[string]any{"inviteLink": invite.Link, "readyAfterMs": 0}, nil
	case "members":
		return a.members(ctx, aid, gid, g, live, s, true)
	case "promote", "kick":
		members, err := a.members(ctx, aid, gid, g, live, s, true)
		_ = members
		if err != nil {
			return nil, err
		}
		ownerPresent := false
		for pid, role := range g.MemberRoles {
			if role == "creator" && g.Members[pid] {
				ownerPresent = true
			}
		}
		if op == "kick" && !ownerPresent {
			return nil, &APIError{409, "OWNER_LEFT", 0}
		}
		callerRole := g.MemberRoles[s.state.Accounts[aid].PlatformID]
		if (op == "promote" && callerRole != "creator") || (op == "kick" && callerRole != "creator" && callerRole != "admin") {
			return nil, &APIError{403, "NO_PERMISSION", 0}
		}
		pid := in.TargetID
		if op == "promote" {
			target := s.state.Accounts[in.AccountID]
			if target == nil || !g.Members[target.PlatformID] {
				return nil, &APIError{409, "NOT_MEMBER_YET", 0}
			}
			pid = target.PlatformID
		}
		user, err := a.participantUser(ctx, channel, live, pid)
		if err != nil {
			return nil, err
		}
		var upd tg.UpdatesClass
		if op == "promote" {
			upd, err = ops.EditChatAdmin(ctx, live.api, &tg.ChannelsEditAdminRequest{Channel: channel, UserID: user, AdminRights: tg.ChatAdminRights{ChangeInfo: true, DeleteMessages: true, BanUsers: true, InviteUsers: true, PinMessages: true, ManageCall: true}, Rank: "Kapibala admin"})
		} else {
			upd, err = live.api.ChannelsEditBanned(ctx, &tg.ChannelsEditBannedRequest{Channel: channel, Participant: &tg.InputPeerUser{UserID: user.UserID, AccessHash: user.AccessHash}, BannedRights: tg.ChatBannedRights{ViewMessages: true}})
		}
		if err != nil {
			return nil, platformError(err)
		}
		a.handleUpdates(aid, upd)
		if op == "promote" {
			g.Admins[in.AccountID] = true
			g.MemberRoles[pid] = "admin"
			return map[string]any{}, nil
		}
		if g.Members[pid] {
			delete(g.Members, pid)
			s.emit("member_left", map[string]any{"groupId": gid, "platformUserId": pid})
		}
		return map[string]bool{"kicked": true}, nil
	case "leave":
		upd, err := ops.LeaveChannel(ctx, live.api, channel)
		if err != nil {
			return nil, platformError(err)
		}
		a.handleUpdates(aid, upd)
		pid := s.state.Accounts[aid].PlatformID
		if g.Members[pid] {
			delete(g.Members, pid)
			s.emit("member_left", map[string]any{"groupId": gid, "platformUserId": pid})
		}
		return map[string]any{}, nil
	case "send":
		if in.ClientID == "" || in.Text == "" {
			return nil, &APIError{400, "INVALID_INPUT", 0}
		}
		acct := s.state.Accounts[aid]
		now := timeNow()
		if now.Before(acct.RateUntil) {
			acct.RateUntil = now.Add(time.Duration(acct.RetrySeconds) * time.Second)
			return nil, &APIError{429, "RATE_LIMITED", acct.RetrySeconds}
		}
		rid, err := randomID()
		if err != nil {
			return nil, err
		}
		key := stringID(rid)
		eff := &effect{ID: key, RandomID: rid, AccountID: aid, GroupID: gid, ClientID: in.ClientID, Text: in.Text, State: "prepared"}
		s.state.Effects[key] = eff
		if err = s.save(ctx); err != nil {
			return nil, err
		}
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		upd, err := ops.SendMessage(callCtx, live.api, &tg.MessagesSendMessageRequest{Peer: peer, Message: in.Text, RandomID: rid})
		if err != nil {
			api := platformError(err)
			eff.State = "unknown"
			if api.Status != 503 && api.Status != 504 {
				eff.State = "failed"
			}
			if api.Code == "RATE_LIMITED" {
				acct.RetrySeconds = api.RetryAfterSeconds
				acct.RateUntil = now.Add(time.Duration(acct.RetrySeconds) * time.Second)
			}
			if api.Code == "ACCOUNT_SUSPENDED" {
				s.terminal(aid, "suspended")
			}
			if api.Code == "SESSION_EXPIRED" {
				s.terminal(aid, "session_expired")
			}
			return nil, api
		}
		eff.State = "accepted"
		if short, ok := upd.(*tg.UpdateShortSentMessage); ok {
			a.finalizeEffect(eff, strconv.Itoa(short.ID), time.Unix(int64(short.Date), 0).UTC())
		} else {
			a.handleUpdates(aid, upd)
		}
		return map[string]bool{"accepted": true}, nil
	default:
		return nil, &APIError{404, "NOT_FOUND", 0}
	}
}

func (a *TelegramAdapter) createGroup(ctx context.Context, aid string, in input, s *Service, live *onlineSession) (any, error) {
	key := in.IdempotencyKey
	if gid := s.state.CreateKeys[key]; key != "" && gid != "" {
		return map[string]string{"groupId": gid}, nil
	}
	title := "Kapibala AI"
	if key != "" {
		sum := sha256.Sum256([]byte(key))
		title = fmt.Sprintf("Kapibala AI %x", sum[:8])
	}
	eid := "create:" + key
	if old := s.state.Effects[eid]; key != "" && old != nil {
		channels, err := live.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: 100})
		if err != nil {
			return nil, platformError(err)
		}
		var chats []tg.ChatClass
		switch v := channels.(type) {
		case *tg.MessagesDialogs:
			chats = v.Chats
		case *tg.MessagesDialogsSlice:
			chats = v.Chats
		}
		for _, chat := range chats {
			if ch, ok := chat.(*tg.Channel); ok && ch.Title == title && ch.Creator {
				return a.registerCreated(aid, key, ch, s), nil
			}
		}
		return nil, &APIError{504, "NETWORK_TIMEOUT", 0}
	}
	if key != "" {
		s.state.Effects[eid] = &effect{ID: eid, AccountID: aid, Text: title, State: "unknown"}
		if err := s.save(ctx); err != nil {
			return nil, err
		}
	}
	upd, err := ops.CreateChannel(ctx, live.api, &tg.ChannelsCreateChannelRequest{Megagroup: true, Title: title, About: "Kapibala AI managed group"})
	if err != nil {
		return nil, platformError(err)
	}
	for _, chat := range updateChats(upd) {
		if ch, ok := chat.(*tg.Channel); ok {
			return a.registerCreated(aid, key, ch, s), nil
		}
	}
	return nil, &APIError{504, "NETWORK_TIMEOUT", 0}
}
func (a *TelegramAdapter) registerCreated(aid, key string, ch *tg.Channel, s *Service) any {
	gid := "tg-" + stringID(ch.ID)
	pid := s.state.Accounts[aid].PlatformID
	s.state.Groups[gid] = &group{Owner: aid, Members: map[string]bool{pid: true}, Admins: map[string]bool{}, Writable: true, ChannelID: ch.ID, AccessHashes: map[string]int64{aid: ch.AccessHash}, MemberRoles: map[string]string{pid: "creator"}}
	if key != "" {
		s.state.CreateKeys[key] = gid
		s.state.Effects["create:"+key].State = "sent"
	}
	return map[string]string{"groupId": gid}
}
func updateChats(upd tg.UpdatesClass) []tg.ChatClass {
	switch u := upd.(type) {
	case *tg.Updates:
		return u.Chats
	case *tg.UpdatesCombined:
		return u.Chats
	}
	return nil
}
func (a *TelegramAdapter) importGroup(ctx context.Context, in input, s *Service) (any, error) {
	var gid string
	for _, aid := range in.AccountIDs {
		live, e := a.online(aid)
		if e != nil {
			return nil, e
		}
		resolved, err := live.api.ContactsResolveUsername(ctx, in.Username)
		if err != nil {
			return nil, platformError(err)
		}
		var channel *tg.Channel
		for _, chat := range resolved.Chats {
			if ch, ok := chat.(*tg.Channel); ok && ch.Megagroup {
				channel = ch
				break
			}
		}
		if channel == nil {
			return nil, &APIError{404, "GROUP_NOT_FOUND", 0}
		}
		current := "tg-" + stringID(channel.ID)
		if gid != "" && gid != current {
			return nil, &APIError{409, "GROUP_IDENTITY_CHANGED", 0}
		}
		gid = current
		g := s.state.Groups[gid]
		if g == nil {
			g = &group{Members: map[string]bool{}, Admins: map[string]bool{}, Writable: true, ChannelID: channel.ID, AccessHashes: map[string]int64{}, MemberRoles: map[string]string{}}
			s.state.Groups[gid] = g
		}
		g.AccessHashes[aid] = channel.AccessHash
		if _, e = a.members(ctx, aid, gid, g, live, s, false); e != nil {
			return nil, e
		}
		pid := s.state.Accounts[aid].PlatformID
		if !g.Members[pid] {
			return nil, &APIError{403, "SENDER_NOT_IN_GROUP", 0}
		}
		if g.MemberRoles[pid] == "creator" {
			g.Owner = aid
		}
		if g.MemberRoles[pid] == "admin" {
			g.Admins[aid] = true
		}
	}
	return map[string]string{"groupId": gid}, nil
}
func (a *TelegramAdapter) participants(ctx context.Context, channel *tg.InputChannel, live *onlineSession) ([]tg.ChannelParticipantClass, map[int64]*tg.User, error) {
	var all []tg.ChannelParticipantClass
	users := map[int64]*tg.User{}
	for offset := 0; ; offset += 200 {
		res, e := ops.GetParticipants(ctx, live.api, &tg.ChannelsGetParticipantsRequest{Channel: channel, Filter: &tg.ChannelParticipantsRecent{}, Offset: offset, Limit: 200})
		if e != nil {
			return nil, nil, platformError(e)
		}
		p, ok := res.(*tg.ChannelsChannelParticipants)
		if !ok {
			return nil, nil, &APIError{503, "GATEWAY_UNAVAILABLE", 0}
		}
		all = append(all, p.Participants...)
		for _, u := range p.Users {
			if user, ok := u.(*tg.User); ok {
				users[user.ID] = user
			}
		}
		if len(p.Participants) < 200 || len(all) >= p.Count {
			return all, users, nil
		}
	}
}
func participantID(p tg.ChannelParticipantClass) (int64, string) {
	switch v := p.(type) {
	case *tg.ChannelParticipant:
		return v.UserID, "member"
	case *tg.ChannelParticipantSelf:
		return v.UserID, "member"
	case *tg.ChannelParticipantCreator:
		return v.UserID, "creator"
	case *tg.ChannelParticipantAdmin:
		return v.UserID, "admin"
	}
	return 0, ""
}
func (a *TelegramAdapter) members(ctx context.Context, aid, gid string, g *group, live *onlineSession, s *Service, events bool) ([]map[string]string, error) {
	ps, _, err := a.participants(ctx, &tg.InputChannel{ChannelID: g.ChannelID, AccessHash: g.AccessHashes[aid]}, live)
	if err != nil {
		return nil, err
	}
	return a.applyMembers(gid, g, s, ps, events), nil
}

// applyMembers shares the snapshot/event merge while its caller holds Service.mu.
func (a *TelegramAdapter) applyMembers(gid string, g *group, s *Service, ps []tg.ChannelParticipantClass, events bool) []map[string]string {
	members := map[string]bool{}
	roles := map[string]string{}
	out := make([]map[string]string, 0, len(ps))
	for _, p := range ps {
		id, role := participantID(p)
		if id == 0 {
			continue
		}
		pid := stringID(id)
		members[pid] = true
		roles[pid] = role
		out = append(out, map[string]string{"platformUserId": pid, "role": role})
		if events && !g.Members[pid] {
			s.emit("member_joined", map[string]any{"groupId": gid, "platformUserId": pid})
		}
	}
	if events {
		for pid := range g.Members {
			if !members[pid] {
				s.emit("member_left", map[string]any{"groupId": gid, "platformUserId": pid})
			}
		}
	}
	g.Members = members
	g.MemberRoles = roles
	sort.Slice(out, func(i, j int) bool { return out[i]["platformUserId"] < out[j]["platformUserId"] })
	return out
}
func (a *TelegramAdapter) participantUser(ctx context.Context, ch *tg.InputChannel, live *onlineSession, pid string) (*tg.InputUser, error) {
	_, users, e := a.participants(ctx, ch, live)
	if e != nil {
		return nil, e
	}
	uid, e := strconv.ParseInt(pid, 10, 64)
	if e != nil {
		return nil, &APIError{400, "INVALID_INPUT", 0}
	}
	u := users[uid]
	if u == nil {
		return nil, &APIError{409, "NOT_MEMBER_YET", 0}
	}
	return &tg.InputUser{UserID: uid, AccessHash: u.AccessHash}, nil
}
