package platform_test

import (
	"bytes"
	"context"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaDownloadAuthRetentionAndSnapshot(t *testing.T) {
	// A real local byte server, not a Telegram/gateway operation simulator.
	data := []byte("binary attachment for retention test")
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="sample.bin"`)
		w.Write(data)
	}))
	defer source.Close()
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: source.URL, MediaDir: t.TempDir()})
	token, _ := login(t, s, "viewer")
	businessSQL(t, cfg, `INSERT INTO accounts(id) VALUES('media-owner'); INSERT INTO groups(id,gateway_group_id,creator_account_id) VALUES('media-group','tg-20','media-owner');
 INSERT INTO messages(id,group_id,msg_id,sender_platform_user_id,is_own,text,sent_at,media_url,media_status) VALUES(901,'media-group','30','40',false,'','2026-01-01T00:00:00Z','/media/source','pending'),(902,'media-group','31','40',false,'newer','2026-01-02T00:00:00Z',NULL,NULL)`)
	if e := platform.DownloadMediaForTest(a, "901"); e != nil {
		t.Fatal(e)
	}
	saved, e := os.ReadFile(filepath.Join(cfg.MediaDir, "901.media"))
	if e != nil || !bytes.Equal(saved, data) {
		t.Fatal("file bytes not persisted", e)
	}
	get := func(auth string) (int, []byte) {
		req, _ := http.NewRequest("GET", s.URL+"/api/media/901", nil)
		req.Header.Set("Authorization", "Bearer "+auth)
		res, e := s.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, body
	}
	if status, _ := get(""); status != 401 {
		t.Fatal("anonymous media access", status)
	}
	if status, body := get(token); status != 200 || !bytes.Equal(body, data) {
		t.Fatal("viewer cannot read attachment", status)
	}
	for _, alias := range []string{"0901", "+901", "../901"} {
		status, _, _ := call(t, s, "GET", "/api/media/"+alias, token, "")
		if status == 200 {
			t.Fatal("noncanonical media path accepted")
		}
	}
	if status, _ := get(token); status != 200 {
		t.Fatal("invalid path damaged stored media")
	}
	_, first, _ := call(t, s, "GET", "/api/groups/media-group/messages?limit=1", token, "")
	cursor := first["nextCursor"].(string)
	businessSQL(t, cfg, `UPDATE messages SET media_downloaded_at=now()-interval '31 days' WHERE id=901; INSERT INTO agent_runs(id,group_id,status,history) VALUES('media-running','media-group','running','[]')`)
	if e = platform.CleanMediaForTest(a); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(cfg.MediaDir, "901.media")); e != nil {
		t.Fatal("running Agent lost its media")
	}
	businessSQL(t, cfg, `UPDATE agent_runs SET status='finished',end_reason='final' WHERE id='media-running'`)
	if e = platform.CleanMediaForTest(a); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(cfg.MediaDir, "901.media")); !os.IsNotExist(e) {
		t.Fatal("expired file not removed")
	}
	_, page, _ := call(t, s, "GET", "/api/groups/media-group/messages?before="+cursor, token, "")
	item := page["items"].([]any)[0].(map[string]any)
	if item["localFilePath"] != nil || item["mediaStatus"] != "expired" {
		t.Fatal("cursor retained deleted file", item)
	}
	if status, _ := get(token); status != 410 {
		t.Fatal("deleted media remained accessible", status)
	}
	status, _, _ := call(t, s, "POST", "/api/media/901/retry", token, "")
	if status != 403 {
		t.Fatal("viewer retry write allowed", status)
	}
	admin, _ := login(t, s, "admin")
	status, _, _ = call(t, s, "POST", "/api/media/901/retry", admin, "")
	if status != 202 {
		t.Fatal("admin cannot retry", status)
	}
	if e = platform.DownloadMediaForTest(a, "901"); e != nil {
		t.Fatal(e)
	}
	if status, body := get(token); status != 200 || !bytes.Equal(body, data) {
		t.Fatal("retry failed", status)
	}
	// Recovery after DB clear / before filesystem unlink.
	businessSQL(t, cfg, `UPDATE messages SET media_status='deleting',local_file_path=NULL WHERE id=901`)
	if e = platform.CleanMediaForTest(a); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(cfg.MediaDir, "901.media")); !os.IsNotExist(e) {
		t.Fatal("interrupted cleanup not resumed")
	}
}

func TestMediaDownloadRejectsRedirectAndOversize(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/media/redirect" {
			http.Redirect(w, r, "http://127.0.0.1:1/private", 302)
			return
		}
		w.Header().Set("Content-Length", "67108865")
		w.WriteHeader(200)
	}))
	defer source.Close()
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: source.URL, MediaDir: t.TempDir()})
	token, _ := login(t, s, "admin")
	businessSQL(t, cfg, `INSERT INTO accounts(id) VALUES('media-owner');INSERT INTO groups(id,creator_account_id) VALUES('media-group','media-owner');INSERT INTO messages(id,group_id,msg_id,sender_platform_user_id,is_own,text,media_url,media_status) VALUES(903,'media-group','33','40',false,'','/media/redirect','pending'),(904,'media-group','34','40',false,'','/media/large','pending')`)
	for _, id := range []string{"903", "904"} {
		if e := platform.DownloadMediaForTest(a, id); e != nil {
			t.Fatal(e)
		}
	}
	_, page, _ := call(t, s, "GET", "/api/groups/media-group/messages", token, "")
	for _, raw := range page["items"].([]any) {
		m := raw.(map[string]any)
		if m["localFilePath"] != nil || m["mediaStatus"] == "ready" || m["mediaError"] == nil {
			t.Fatal("invalid download accepted", m)
		}
	}
}

func TestMediaEventSurvivesIngestionWithoutBlockingText(t *testing.T) {
	a, s, cfg := fixtureConfig(t)
	token, _ := login(t, s, "admin")
	businessSQL(t, cfg, `INSERT INTO accounts(id) VALUES('media-owner'); INSERT INTO groups(id,gateway_group_id,creator_account_id) VALUES('media-group','tg-20','media-owner');
 INSERT INTO gateway_inbox(event_id,type,data,received_at) VALUES(1,'message','{"groupId":"tg-20","msgId":"30","senderPlatformUserId":"40","text":"","sentAt":"2026-01-01T00:00:00Z","mediaUrl":"/media/tg-20_30","mediaKind":"sticker","mediaMime":"image/webp","mediaName":"sticker.webp","mediaSize":123}',now()-interval '2 seconds'),(2,'message','{"groupId":"tg-20","msgId":"31","senderPlatformUserId":"40","text":"still responsive","sentAt":"2026-01-01T00:00:01Z"}',now()-interval '2 seconds')`)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var items []any
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		_, b, _ := call(t, s, "GET", "/api/groups/media-group/messages", token, "")
		items = b["items"].([]any)
		if len(items) == 2 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if len(items) != 2 {
		t.Fatal("media download blocked inbox")
	}
	m := items[1].(map[string]any)
	if m["mediaUrl"] != "/media/tg-20_30" || m["mediaKind"] != "sticker" || m["mediaStatus"] == nil {
		t.Fatalf("media disappeared: %v", m)
	}
	if m["localFilePath"] != nil {
		t.Fatal("unavailable gateway must not create a successful download")
	}
}
