package gateway

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/lbtlm/KapibalaAI_gotd/gateway/internal/ops"
	"time"
)

// ProbeResult discloses aliases and capabilities without user IDs or secrets.
type ProbeResult struct {
	AccountID     string `json:"accountId"`
	Authorized    bool   `json:"authorized"`
	GroupResolved bool   `json:"groupResolved"`
	GroupID       string `json:"groupId,omitempty"`
	Role          string `json:"role,omitempty"`
	CanKick       bool   `json:"canKick"`
	Code          string `json:"code,omitempty"`
	AttemptedJoin bool   `json:"attemptedJoin,omitempty"`
	DisplayName   string `json:"displayName,omitempty"`
	Username      string `json:"username,omitempty"`
}

// ProbeAccounts performs only Self, ResolveUsername and GetParticipant(self).
// NoUpdates prevents SDK difference/history calls or unrelated message ingestion.
func ProbeAccounts(ctx context.Context, db *sql.DB, configs []AccountConfig, username string) []ProbeResult {
	return probeAccounts(ctx, db, configs, username, false)
}

// JoinTestGroup makes exactly one explicit join attempt per supplied isolated account.
func JoinTestGroup(ctx context.Context, db *sql.DB, configs []AccountConfig, username string) []ProbeResult {
	return probeAccounts(ctx, db, configs, username, true)
}
func probeAccounts(ctx context.Context, db *sql.DB, configs []AccountConfig, username string, join bool) []ProbeResult {
	results := make([]ProbeResult, 0, len(configs))
	for _, c := range configs {
		r := ProbeResult{AccountID: c.ID}
		storage := dbSession{db, c.ID}
		if _, e := storage.LoadSession(ctx); errors.Is(e, session.ErrNotFound) {
			b, e := ImportSession(c.SessionPath, c.SessionFormat, c.Passcode)
			if e != nil {
				r.Code = "SESSION_IMPORT_FAILED"
				results = append(results, r)
				continue
			}
			if e = storage.StoreSession(ctx, b); e != nil {
				r.Code = "SESSION_STORAGE_FAILED"
				results = append(results, r)
				continue
			}
		} else if e != nil {
			r.Code = "SESSION_STORAGE_FAILED"
			results = append(results, r)
			continue
		}
		client := telegram.NewClient(c.APIID, c.APIHash, telegram.Options{SessionStorage: storage, NoUpdates: true, MigrationTimeout: 120 * time.Second, Device: telegram.DeviceConfig{DeviceModel: c.DeviceModel, SystemVersion: c.SystemVersion, AppVersion: c.AppVersion, LangCode: "en", SystemLangCode: "en-US"}})
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		e := client.Run(probeCtx, func(live context.Context) error {
			self, e := client.Self(live)
			if e != nil {
				return e
			}
			r.Authorized = true
			r.DisplayName = self.FirstName + " " + self.LastName
			r.Username = self.Username
			api := tg.NewClient(client)
			resolved, e := api.ContactsResolveUsername(live, username)
			if e != nil {
				return e
			}
			for _, chat := range resolved.Chats {
				ch, ok := chat.(*tg.Channel)
				if !ok || !ch.Megagroup {
					continue
				}
				r.GroupResolved = true
				r.GroupID = "tg-" + stringID(ch.ID)
				if join {
					r.AttemptedJoin = true
					if _, e = ops.JoinChannel(live, api, &tg.InputChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}); e != nil {
						return e
					}
				}
				participant, e := api.ChannelsGetParticipant(live, &tg.ChannelsGetParticipantRequest{Channel: &tg.InputChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, Participant: &tg.InputPeerSelf{}})
				if e != nil {
					return e
				}
				_, r.Role = participantID(participant.Participant)
				switch p := participant.Participant.(type) {
				case *tg.ChannelParticipantCreator:
					r.CanKick = true
				case *tg.ChannelParticipantAdmin:
					r.CanKick = p.AdminRights.BanUsers
				}
				_ = self
				return nil
			}
			return &APIError{404, "GROUP_NOT_FOUND", 0}
		})
		cancel()
		if e != nil {
			r.Code = platformError(e).Code
			if rpc, ok := tgerr.As(e); ok {
				r.Code = rpc.Type
			}
		}
		results = append(results, r)
	}
	return results
}
