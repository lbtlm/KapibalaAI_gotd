package platform

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *App) ws(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, e := url.Parse(origin)
		if e != nil {
			return false
		}
		host := strings.Split(r.Host, ":")[0]
		return u.Host == r.Host || ((u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") && (host == "localhost" || host == "127.0.0.1"))
	}}
	c, e := upgrader.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer c.Close()
	c.SetReadLimit(8192)
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var auth struct {
		Type        string `json:"type"`
		AccessToken string `json:"accessToken"`
		SinceSeq    int64  `json:"sinceSeq"`
	}
	if c.ReadJSON(&auth) != nil {
		return
	}
	if auth.Type != "auth" || auth.SinceSeq < 0 {
		return
	}
	if _, e = a.authorize(r.Context(), auth.AccessToken); e != nil {
		c.WriteJSON(map[string]any{"type": "auth", "success": false})
		return
	}
	if c.WriteJSON(map[string]any{"type": "auth", "success": true}) != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	// A reader notices disconnects. All writes are serialized by this loop.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, e := c.ReadMessage(); e != nil {
				return
			}
		}
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	seq := auth.SinceSeq
	for {
		if _, e = a.authorize(r.Context(), auth.AccessToken); e != nil {
			return
		}
		rows, e := a.pool.Query(r.Context(), `SELECT seq,type,payload FROM ws_events WHERE seq>$1 ORDER BY seq LIMIT 200`, seq)
		if e == nil {
			type frame struct {
				Seq     int64           `json:"seq"`
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			frames := []frame{}
			for rows.Next() {
				var f frame
				if rows.Scan(&f.Seq, &f.Type, &f.Payload) == nil {
					frames = append(frames, f)
				}
			}
			rows.Close()
			for _, f := range frames {
				c.SetWriteDeadline(time.Now().Add(3 * time.Second))
				if c.WriteJSON(f) != nil {
					return
				}
				seq = f.Seq
			}
		}
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}
