package gateway

import "time"

// Event is a durable gateway SSE frame; the data repeats its envelope fields.
type Event struct {
	EventID int64          `json:"eventId"`
	Type    string         `json:"type"`
	Data    map[string]any `json:"data"`
}
type account struct {
	PlatformID   string    `json:"platformId"`
	Status       string    `json:"status"`
	RateUntil    time.Time `json:"rateUntil"`
	RetrySeconds int       `json:"retrySeconds"`
}
type group struct {
	Owner         string            `json:"owner"`
	Members       map[string]bool   `json:"members"`
	Admins        map[string]bool   `json:"admins"`
	Invite        string            `json:"invite"`
	InviteReady   time.Time         `json:"inviteReady"`
	InviteExpired bool              `json:"inviteExpired"`
	Writable      bool              `json:"writable"`
	ChannelID     int64             `json:"channelId"`
	AccessHashes  map[string]int64  `json:"accessHashes"`
	MemberRoles   map[string]string `json:"memberRoles"`
}
type message struct {
	GroupID  string    `json:"groupId"`
	ClientID string    `json:"clientId"`
	MsgID    string    `json:"msgId"`
	Sender   string    `json:"sender"`
	Text     string    `json:"text"`
	SentAt   time.Time `json:"sentAt"`
}
type job struct {
	Kind       string         `json:"kind"`
	Due        time.Time      `json:"due"`
	GroupID    string         `json:"groupId"`
	AccountID  string         `json:"accountId"`
	PlatformID string         `json:"platformId"`
	ClientID   string         `json:"clientId"`
	Text       string         `json:"text"`
	Code       string         `json:"code"`
	Data       map[string]any `json:"data,omitempty"`
}

// Fault controls one explicitly enabled fake failure, optionally scoped.
type Fault struct {
	Operation         string `json:"operation"`
	AccountID         string `json:"accountId,omitempty"`
	GroupID           string `json:"groupId,omitempty"`
	Code              string `json:"code"`
	Count             int    `json:"count"`
	RetryAfterSeconds int    `json:"retryAfterSeconds,omitempty"`
	SettleAfterMs     int    `json:"settleAfterMs,omitempty"`
	Effect            bool   `json:"effect"`
}
type delivery struct {
	Duplicate       bool `json:"duplicate"`
	Reorder         bool `json:"reorder"`
	DisconnectAfter int  `json:"disconnectAfter"`
}
type effect struct {
	ID        string    `json:"id"`
	RandomID  int64     `json:"randomId"`
	AccountID string    `json:"accountId"`
	GroupID   string    `json:"groupId"`
	ClientID  string    `json:"clientId"`
	Text      string    `json:"text"`
	State     string    `json:"state"`
	MsgID     string    `json:"msgId"`
	SentAt    time.Time `json:"sentAt"`
}
type state struct {
	Mode        string              `json:"mode"`
	Accounts    map[string]*account `json:"accounts"`
	Groups      map[string]*group   `json:"groups"`
	Messages    []message           `json:"messages"`
	Events      []Event             `json:"events"`
	Jobs        []job               `json:"jobs"`
	Faults      []Fault             `json:"faults"`
	Delivery    delivery            `json:"delivery"`
	Effects     map[string]*effect  `json:"effects"`
	SeenUpdates map[string]bool     `json:"seenUpdates"`
	NextID      int64               `json:"nextId"`
	NextEvent   int64               `json:"nextEvent"`
	CreateKeys  map[string]string   `json:"createKeys"`
}

func initialState() state {
	s := state{Accounts: map[string]*account{}, Groups: map[string]*group{}, Effects: map[string]*effect{}, SeenUpdates: map[string]bool{}, CreateKeys: map[string]string{}}
	for i, id := range []string{"account-1", "account-2", "account-3"} {
		s.Accounts[id] = &account{PlatformID: stringID(10001 + int64(i)), Status: "idle"}
	}
	return s
}
