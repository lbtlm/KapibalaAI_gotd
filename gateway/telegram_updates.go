package gateway

import (
	"github.com/gotd/td/tg"
	"strconv"
	"time"
)

// Adapted from online_manager_updates.go:11; outgoing updates are retained.
func (a *TelegramAdapter) handleUpdates(aid string, u tg.UpdatesClass) {
	if a.service == nil {
		return
	}
	s := a.service
	if s.state.Accounts[aid] == nil {
		return
	}
	for _, chat := range updateChats(u) {
		if ch, ok := chat.(*tg.Channel); ok {
			if g := s.state.Groups["tg-"+stringID(ch.ID)]; g != nil {
				g.AccessHashes[aid] = ch.AccessHash
			}
		}
	}
	var updates []tg.UpdateClass
	switch v := u.(type) {
	case *tg.Updates:
		updates = v.Updates
	case *tg.UpdatesCombined:
		updates = v.Updates
	case *tg.UpdateShort:
		updates = []tg.UpdateClass{v.Update}
	}
	// Random-id correlations must be recorded before new-message updates.
	for _, up := range updates {
		if v, ok := up.(*tg.UpdateMessageID); ok {
			for _, e := range s.state.Effects {
				if e.AccountID == aid && e.RandomID == v.RandomID {
					e.MsgID = strconv.Itoa(v.ID)
					for _, old := range s.state.Events {
						if old.Type != "message" || old.Data["groupId"] != e.GroupID || old.Data["msgId"] != e.MsgID || old.Data["senderPlatformUserId"] != s.state.Accounts[aid].PlatformID {
							continue
						}
						var when time.Time
						switch value := old.Data["sentAt"].(type) {
						case time.Time:
							when = value
						case string:
							when, _ = time.Parse(time.RFC3339Nano, value)
						}
						if !when.IsZero() {
							a.finalizeEffect(e, e.MsgID, when)
						}
						break
					}
				}
			}
		}
	}
	for _, up := range updates {
		switch v := up.(type) {
		case *tg.UpdateNewChannelMessage:
			a.platformMessage(aid, v.Message)
		case *tg.UpdateNewMessage:
			a.platformMessage(aid, v.Message)
		case *tg.UpdateChannelParticipant:
			gid := "tg-" + stringID(v.ChannelID)
			g := s.state.Groups[gid]
			if g == nil {
				continue
			}
			pid := stringID(v.UserID)
			_, role := participantID(v.NewParticipant)
			if role != "" {
				g.MemberRoles[pid] = role
				if !g.Members[pid] {
					g.Members[pid] = true
					s.emit("member_joined", map[string]any{"groupId": gid, "platformUserId": pid})
				}
			} else if g.Members[pid] {
				delete(g.Members, pid)
				s.emit("member_left", map[string]any{"groupId": gid, "platformUserId": pid})
			}
		}
	}
	for _, g := range s.state.Groups {
		g.confirmObservedJoins(s.state.Accounts)
	}
}
func (a *TelegramAdapter) platformMessage(aid string, mc tg.MessageClass) {
	s := a.service
	var cid int64
	var mid int
	var date int
	switch m := mc.(type) {
	case *tg.Message:
		if p, ok := m.PeerID.(*tg.PeerChannel); ok {
			cid = p.ChannelID
		}
		mid = m.ID
		date = m.Date
	case *tg.MessageService:
		if p, ok := m.PeerID.(*tg.PeerChannel); ok {
			cid = p.ChannelID
		}
		g := s.state.Groups["tg-"+stringID(cid)]
		if g == nil {
			return
		}
		switch action := m.Action.(type) {
		case *tg.MessageActionChatAddUser:
			for _, uid := range action.Users {
				pid := stringID(uid)
				if !g.Members[pid] {
					g.Members[pid] = true
					s.emit("member_joined", map[string]any{"groupId": "tg-" + stringID(cid), "platformUserId": pid})
				}
			}
		case *tg.MessageActionChatDeleteUser:
			pid := stringID(action.UserID)
			if g.Members[pid] {
				delete(g.Members, pid)
				s.emit("member_left", map[string]any{"groupId": "tg-" + stringID(cid), "platformUserId": pid})
			}
		}
		return
	default:
		return
	}
	gid := "tg-" + stringID(cid)
	if s.state.Groups[gid] == nil {
		return
	}
	msgID := strconv.Itoa(mid)
	sentAt := time.Unix(int64(date), 0).UTC()
	for _, e := range s.state.Effects {
		if e.AccountID == aid && e.GroupID == gid && e.MsgID == msgID && e.State != "sent" {
			a.finalizeEffect(e, msgID, sentAt)
		}
	}
	key := gid + ":" + msgID
	if s.state.SeenUpdates[key] {
		return
	}
	s.state.SeenUpdates[key] = true
	m, ok := mc.(*tg.Message)
	if !ok {
		return
	}
	sender := ""
	if p, ok := m.FromID.(*tg.PeerUser); ok {
		sender = stringID(p.UserID)
	} else if m.Out {
		sender = s.state.Accounts[aid].PlatformID
	}
	s.emit("message", map[string]any{"groupId": gid, "msgId": msgID, "senderPlatformUserId": sender, "text": m.Message, "sentAt": sentAt})
}
func (a *TelegramAdapter) finalizeEffect(e *effect, msgID string, sentAt time.Time) {
	s := a.service
	if e.State == "sent" {
		return
	}
	e.State = "sent"
	e.MsgID = msgID
	e.SentAt = sentAt
	s.state.Messages = append(s.state.Messages, message{e.GroupID, e.ClientID, msgID, s.state.Accounts[e.AccountID].PlatformID, e.Text, sentAt})
	s.emit("message_sent", map[string]any{"groupId": e.GroupID, "clientMsgId": e.ClientID, "msgId": msgID, "sentAt": sentAt})
	key := e.GroupID + ":" + msgID
	if !s.state.SeenUpdates[key] {
		s.state.SeenUpdates[key] = true
		s.emit("message", map[string]any{"groupId": e.GroupID, "msgId": msgID, "senderPlatformUserId": s.state.Accounts[e.AccountID].PlatformID, "text": e.Text, "sentAt": sentAt})
	}
}
