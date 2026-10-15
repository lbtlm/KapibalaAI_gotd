package gateway

import (
	"context"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	sdkstorage "github.com/lbtlm/KapibalaAI_gotd/gateway/internal/storage"
	"sort"
	"sync"
	"time"
)

// UpdatePipeline exposes the public gotd update boundaries as one account binding.
type UpdatePipeline struct {
	API              updates.API
	Storage          updates.StateStorage
	AccessHasher     updates.ChannelAccessHasher
	Handler          telegram.UpdateHandler
	OnChannelTooLong func(int64)
	gate             *ingestGate
}

// Run is the account pipeline's actual SDK lifecycle boundary.
func (p *UpdatePipeline) Run(ctx context.Context, manager *updates.Manager, uid int64, opt updates.AuthOptions) error {
	d := p.API.(*durableDifferenceAPI)
	d.adapter.service.mu.Lock()
	var channels []int64
	knownHashes := map[int64]int64{}
	for _, g := range d.adapter.service.state.Groups {
		if g.AccessHashes[d.aid] != 0 {
			channels = append(channels, g.ChannelID)
			knownHashes[g.ChannelID] = g.AccessHashes[d.aid]
		}
	}
	d.adapter.service.mu.Unlock()
	sort.Slice(channels, func(i, j int) bool { return channels[i] < channels[j] })
	for _, cid := range channels {
		_, hashFound, e := p.AccessHasher.GetChannelAccessHash(ctx, uid, cid)
		if e != nil {
			return e
		}
		if !hashFound {
			if e = p.AccessHasher.SetChannelAccessHash(ctx, uid, cid, knownHashes[cid]); e != nil {
				d.gate.fail(e)
				return e
			}
		}
		_, found, e := p.Storage.GetChannelPts(ctx, uid, cid)
		if e != nil {
			return e
		}
		if found {
			continue
		}
		pts, e := d.recoverChannelCheckpoint(ctx, cid, true)
		if e != nil {
			d.adapter.noteChannelGap(d.aid, cid)
			d.gate.fail(e)
			return e
		}
		if e = p.Storage.SetChannelPts(ctx, uid, cid, pts); e != nil {
			d.gate.fail(e)
			return e
		}
	}
	return manager.Run(ctx, p.API, uid, opt)
}

// Updates constructs this account's SDK API, durable storage and event handler.
func (a *TelegramAdapter) Updates(aid string, api *tg.Client, cancel context.CancelFunc) *UpdatePipeline {
	gate := &ingestGate{cancel: cancel}
	storage := sdkstorage.UpdateState{DB: a.db, AccountID: aid, Owner: a.service.store, Gate: gate}
	handler := telegram.UpdateHandlerFunc(func(ctx context.Context, u tg.UpdatesClass) error {
		if e := gate.Err(); e != nil {
			return e
		}
		s := a.service
		s.mu.Lock()
		defer s.mu.Unlock()
		opCtx, stop, e := s.operationContext(ctx)
		if e != nil {
			gate.fail(e)
			return e
		}
		defer stop()
		a.handleUpdates(aid, u)
		a.recordSDKRange(aid, u)
		if e = s.save(opCtx); e != nil {
			gate.fail(e)
			s.cancel()
			return e
		}
		return nil
	})
	difference := &durableDifferenceAPI{api: api, handler: handler, adapter: a, aid: aid, gate: gate}
	return &UpdatePipeline{API: difference, Storage: storage, AccessHasher: storage, Handler: handler, gate: gate, OnChannelTooLong: func(cid int64) {
		if !gate.beginGap(cid) {
			return
		}
		a.service.wg.Add(1)
		go func() {
			defer a.service.wg.Done()
			defer gate.endGap(cid)
			if e := difference.recoverCurrentChannel(a.service.ctx, cid); e != nil {
				a.noteChannelGap(aid, cid)
				gate.fail(e)
			}
		}()
	}}
}

type ingestGate struct {
	mu      sync.Mutex
	failure error
	cancel  context.CancelFunc
	gaps    map[int64]bool
}

func (g *ingestGate) Err() error { g.mu.Lock(); defer g.mu.Unlock(); return g.failure }
func (g *ingestGate) fail(e error) {
	g.mu.Lock()
	if g.failure == nil {
		g.failure = e
		g.cancel()
	}
	g.mu.Unlock()
}
func (g *ingestGate) beginGap(cid int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failure != nil || g.gaps[cid] {
		return false
	}
	if g.gaps == nil {
		g.gaps = map[int64]bool{}
	}
	g.gaps[cid] = true
	return true
}
func (g *ingestGate) endGap(cid int64) { g.mu.Lock(); delete(g.gaps, cid); g.mu.Unlock() }

type durableDifferenceAPI struct {
	api     *tg.Client
	handler telegram.UpdateHandler
	adapter *TelegramAdapter
	aid     string
	gate    *ingestGate
}

func (d *durableDifferenceAPI) UpdatesGetState(ctx context.Context) (*tg.UpdatesState, error) {
	return d.api.UpdatesGetState(ctx)
}
func updatesForMessages(messages []tg.MessageClass) []tg.UpdateClass {
	out := make([]tg.UpdateClass, 0, len(messages))
	for _, message := range messages {
		var peer tg.PeerClass
		switch m := message.(type) {
		case *tg.Message:
			peer = m.PeerID
		case *tg.MessageService:
			peer = m.PeerID
		}
		if _, channel := peer.(*tg.PeerChannel); channel {
			out = append(out, &tg.UpdateNewChannelMessage{Message: message})
		} else {
			out = append(out, &tg.UpdateNewMessage{Message: message})
		}
	}
	return out
}
func (d *durableDifferenceAPI) UpdatesGetDifference(ctx context.Context, req *tg.UpdatesGetDifferenceRequest) (tg.UpdatesDifferenceClass, error) {
	diff, e := d.api.UpdatesGetDifference(ctx, req)
	if e != nil {
		return nil, e
	}
	var payload *tg.Updates
	switch v := diff.(type) {
	case *tg.UpdatesDifference:
		payload = &tg.Updates{Updates: append(updatesForMessages(v.NewMessages), v.OtherUpdates...), Chats: v.Chats, Users: v.Users}
	case *tg.UpdatesDifferenceSlice:
		payload = &tg.Updates{Updates: append(updatesForMessages(v.NewMessages), v.OtherUpdates...), Chats: v.Chats, Users: v.Users}
	case *tg.UpdatesDifferenceTooLong:
		d.adapter.service.mu.Lock()
		var channels []int64
		for _, g := range d.adapter.service.state.Groups {
			if g.AccessHashes[d.aid] != 0 {
				channels = append(channels, g.ChannelID)
			}
		}
		d.adapter.service.mu.Unlock()
		sort.Slice(channels, func(i, j int) bool { return channels[i] < channels[j] })
		for _, cid := range channels {
			if e = d.recoverCurrentChannel(ctx, cid); e != nil {
				d.adapter.noteChannelGap(d.aid, cid)
				d.gate.fail(e)
				return nil, e
			}
		}
	}
	if payload != nil {
		if e = d.handler.Handle(ctx, payload); e != nil {
			return nil, e
		}
	}
	return diff, nil
}
func (d *durableDifferenceAPI) recoverCurrentChannel(parent context.Context, cid int64) error {
	_, e := d.recoverChannelCheckpoint(parent, cid, false)
	return e
}
func (d *durableDifferenceAPI) recoverChannelCheckpoint(parent context.Context, cid int64, verifiedOnly bool) (int, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	s := d.adapter.service
	s.mu.Lock()
	g := s.state.Groups["tg-"+stringID(cid)]
	if g == nil {
		s.mu.Unlock()
		return 0, nil
	}
	hash := g.AccessHashes[d.aid]
	s.mu.Unlock()
	opCtx, stop, e := s.operationContext(ctx)
	if e != nil {
		return 0, e
	}
	dialogs, e := d.api.MessagesGetPeerDialogs(opCtx, []tg.InputDialogPeerClass{&tg.InputDialogPeer{Peer: &tg.InputPeerChannel{ChannelID: cid, AccessHash: hash}}})
	stop()
	if e != nil {
		return 0, &APIError{503, "CHANNEL_HISTORY_UNAVAILABLE", 0}
	}
	for _, item := range dialogs.Dialogs {
		if dialog, ok := item.(*tg.Dialog); ok {
			if peer, ok := dialog.Peer.(*tg.PeerChannel); ok && peer.ChannelID == cid {
				if dialog.Pts <= 0 {
					return 0, &APIError{503, "CHANNEL_HISTORY_INCOMPLETE", 0}
				}
				return dialog.Pts, d.adapter.recoverChannelRange(ctx, d.aid, d.api, &tg.InputChannel{ChannelID: cid, AccessHash: hash}, dialog.TopMessage, verifiedOnly)
			}
		}
	}
	return 0, &APIError{503, "CHANNEL_HISTORY_INCOMPLETE", 0}
}
func (d *durableDifferenceAPI) UpdatesGetChannelDifference(ctx context.Context, req *tg.UpdatesGetChannelDifferenceRequest) (tg.UpdatesChannelDifferenceClass, error) {
	diff, e := d.api.UpdatesGetChannelDifference(ctx, req)
	if e != nil {
		return nil, e
	}
	switch v := diff.(type) {
	case *tg.UpdatesChannelDifference:
		if e = d.handler.Handle(ctx, &tg.Updates{Updates: append(updatesForMessages(v.NewMessages), v.OtherUpdates...), Chats: v.Chats, Users: v.Users}); e != nil {
			return nil, e
		}
	case *tg.UpdatesChannelDifferenceTooLong:
		dialog, ok := v.Dialog.(*tg.Dialog)
		channel, valid := req.Channel.(*tg.InputChannel)
		if !ok || !valid {
			e = &APIError{503, "CHANNEL_HISTORY_INCOMPLETE", 0}
		} else {
			e = d.adapter.recoverChannelRange(ctx, d.aid, d.api, channel, dialog.TopMessage, false)
		}
		if e != nil {
			d.gate.fail(e)
			return nil, e
		}
	}
	return diff, nil
}
