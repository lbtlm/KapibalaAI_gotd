package platform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const mediaLimit int64 = 64 << 20

var mediaRoute = regexp.MustCompile(`^/media/[A-Za-z0-9_-]+$`)

// Only the configured gateway's media endpoint may be fetched. Redirects are
// disabled as well; event payloads cannot turn the downloader into an open proxy.
func mediaSource(base, raw string) (string, error) {
	b, e := url.Parse(base)
	if e != nil {
		return "", e
	}
	u, e := url.Parse(raw)
	if e != nil {
		return "", e
	}
	if !mediaRoute.MatchString(u.Path) || u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.Opaque != "" {
		return "", errors.New("MEDIA_URL_INVALID")
	}
	if u.IsAbs() || u.Host != "" {
		if u.Scheme != b.Scheme || u.Host != b.Host {
			return "", errors.New("MEDIA_URL_INVALID")
		}
	}
	b.Path = u.Path
	b.RawPath = ""
	b.RawQuery = ""
	b.Fragment = ""
	return b.String(), nil
}
func (a *App) populateMedia(ctx context.Context, gid string, items []Message) error {
	ids := []string{}
	for _, m := range items {
		if m.MsgID != nil {
			ids = append(ids, *m.MsgID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, e := a.pool.Query(ctx, `SELECT msg_id,id::text,media_url,media_kind,media_mime,media_name,media_size,media_status,media_error,local_file_path FROM messages WHERE group_id=$1 AND msg_id=ANY($2) AND media_url IS NOT NULL`, gid, ids)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var m Message
		if e = rows.Scan(&id, &m.MediaID, &m.MediaURL, &m.MediaKind, &m.MediaMime, &m.MediaName, &m.MediaSize, &m.MediaStatus, &m.MediaError, &m.LocalFilePath); e != nil {
			return e
		}
		for i := range items {
			if items[i].MsgID != nil && *items[i].MsgID == id {
				v := &items[i]
				v.MediaID = m.MediaID
				v.MediaURL = m.MediaURL
				v.MediaKind = m.MediaKind
				v.MediaMime = m.MediaMime
				v.MediaName = m.MediaName
				v.MediaSize = m.MediaSize
				v.MediaStatus = m.MediaStatus
				v.MediaError = m.MediaError
				v.LocalFilePath = m.LocalFilePath
			}
		}
	}
	return rows.Err()
}
func (a *App) mediaTick(ctx context.Context) {
	for _, id := range a.ids(ctx, `SELECT id::text FROM messages WHERE media_status='pending' AND media_retry_at<=now() ORDER BY id LIMIT 8`) {
		a.launchWorker(ctx, "media:"+id, func(ctx context.Context) error { return a.downloadMedia(ctx, id) })
	}
	// Local retention never performs network calls and shares the existing DB lease.
	_ = a.lock(ctx, "media-cleanup", a.cleanMedia)
}
func (a *App) mediaFailure(ctx context.Context, id, code string, terminal bool) error {
	status := "pending"
	if terminal {
		status = "expired"
	}
	if code == "MEDIA_URL_INVALID" || code == "MEDIA_TOO_LARGE" {
		status = "failed"
	}
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var gid string
	e = tx.QueryRow(ctx, `UPDATE messages SET media_status=CASE WHEN $3='pending' AND media_attempts>=5 THEN 'failed' ELSE $3 END,media_error=$2,media_retry_at=now()+LEAST(300,5*media_attempts*media_attempts)*interval '1 second' WHERE id=$1 AND media_status='pending' RETURNING group_id`, id, code, status).Scan(&gid)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if e = emit(ctx, tx, "message", map[string]any{"groupId": gid, "mediaId": id}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (a *App) downloadMedia(ctx context.Context, id string) error {
	workCtx := ctx
	var raw, gid string
	e := a.pool.QueryRow(ctx, `UPDATE messages SET media_attempts=media_attempts+1 WHERE id=$1 AND media_status='pending' RETURNING media_url,group_id`, id).Scan(&raw, &gid)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	source, e := mediaSource(a.cfg.GatewayURL, raw)
	if e != nil {
		return a.mediaFailure(workCtx, id, "MEDIA_URL_INVALID", false)
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", source, nil)
	if e != nil {
		return e
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return a.mediaFailure(workCtx, id, "MEDIA_DOWNLOAD_FAILED", false)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		code := "MEDIA_DOWNLOAD_FAILED"
		var payload struct {
			Error struct {
				Code string `json:"code"`
			}
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&payload)
		for _, v := range []string{payload.Code, payload.Error.Code} {
			switch v {
			case "ACCOUNT_OFFLINE", "MEDIA_EXPIRED", "MEDIA_TOO_LARGE", "NETWORK_TIMEOUT", "SESSION_EXPIRED", "AUTH_KEY_DUPLICATED", "MEDIA_INCOMPLETE":
				code = v
			}
		}
		return a.mediaFailure(workCtx, id, code, res.StatusCode == 404)
	}
	if res.ContentLength > mediaLimit {
		return a.mediaFailure(workCtx, id, "MEDIA_TOO_LARGE", false)
	}
	f, e := os.CreateTemp(a.cfg.MediaDir, ".partial-*")
	if e != nil {
		return a.mediaFailure(workCtx, id, "MEDIA_STORAGE_ERROR", false)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	size, e := io.Copy(f, io.LimitReader(res.Body, mediaLimit+1))
	if size > mediaLimit {
		return a.mediaFailure(workCtx, id, "MEDIA_TOO_LARGE", false)
	}
	if e != nil || res.ContentLength >= 0 && size != res.ContentLength {
		return a.mediaFailure(workCtx, id, "MEDIA_INCOMPLETE", false)
	}
	if e = f.Sync(); e != nil {
		return a.mediaFailure(workCtx, id, "MEDIA_STORAGE_ERROR", false)
	}
	if e = f.Close(); e != nil {
		return a.mediaFailure(workCtx, id, "MEDIA_STORAGE_ERROR", false)
	}
	name := id + ".media"
	target := filepath.Join(a.cfg.MediaDir, name)
	// A previous process may have renamed a file but died before its DB commit.
	if e = os.Remove(target); e != nil && !os.IsNotExist(e) {
		return a.mediaFailure(workCtx, id, "MEDIA_STORAGE_ERROR", false)
	}
	if e = os.Rename(f.Name(), target); e != nil {
		return a.mediaFailure(workCtx, id, "MEDIA_STORAGE_ERROR", false)
	}
	kind := res.Header.Get("X-Media-Kind")
	switch kind {
	case "photo", "video", "audio", "sticker":
	default:
		kind = "file"
	}
	contentType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, params, _ := mime.ParseMediaType(res.Header.Get("Content-Disposition"))
	filename := filepath.Base(strings.ReplaceAll(params["filename"], "\\", "/"))
	if filename == "." || filename == "" {
		filename = "attachment"
	}
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `UPDATE messages SET local_file_path=$2,media_kind=$3,media_mime=$4,media_name=$5,media_size=$6,media_status='ready',media_error=NULL,media_downloaded_at=now() WHERE id=$1 AND media_status='pending'`, id, "media/"+name, kind, contentType, filename, size)
	if e != nil {
		return e
	}
	if e = emit(ctx, tx, "message", map[string]any{"groupId": gid, "mediaId": id}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (a *App) cleanMedia(ctx context.Context) error {
	ids := a.ids(ctx, `SELECT id::text FROM messages m WHERE media_status='deleting' OR (media_status='ready' AND media_downloaded_at<now()-$1::double precision*interval '1 second' AND NOT EXISTS(SELECT 1 FROM agent_runs r WHERE r.group_id=m.group_id AND r.status='running')) ORDER BY id LIMIT 16`, a.cfg.MediaRetention.Seconds())
	for _, id := range ids {
		tx, e := a.pool.Begin(ctx)
		if e != nil {
			return e
		}
		var gid, status string
		// Agent creation also locks the group. Conservatively protect all files in a
		// group with a running run, including attachments read by later tool calls.
		e = tx.QueryRow(ctx, `SELECT g.id,m.media_status FROM groups g JOIN messages m ON m.group_id=g.id WHERE m.id=$1 FOR UPDATE OF g,m`, id).Scan(&gid, &status)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		var running bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE group_id=$1 AND status='running')`, gid).Scan(&running)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if running {
			tx.Rollback(ctx)
			continue
		}
		_, e = tx.Exec(ctx, `UPDATE messages SET local_file_path=NULL,media_status='deleting',media_error='MEDIA_EXPIRED' WHERE id=$1`, id)
		if e == nil {
			e = emit(ctx, tx, "message", map[string]any{"groupId": gid, "mediaId": id})
		}
		if e == nil {
			e = tx.Commit(ctx)
		} else {
			tx.Rollback(ctx)
		}
		if e != nil {
			return e
		}
		// Clear DB references BEFORE unlinking. 'deleting' is a durable retry marker.
		e = os.Remove(filepath.Join(a.cfg.MediaDir, id+".media"))
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if _, e = a.pool.Exec(ctx, `UPDATE messages SET media_status='expired' WHERE id=$1 AND media_status='deleting'`, id); e != nil {
			return e
		}
	}
	return nil
}
func (a *App) mediaGet(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if n, e := strconv.ParseInt(id, 10, 64); e != nil || n <= 0 || strconv.FormatInt(n, 10) != id {
		return fail(404, "MEDIA_NOT_FOUND")
	}
	var status string
	var name, contentType *string
	e := a.pool.QueryRow(r.Context(), `SELECT media_status,media_name,media_mime FROM messages WHERE id=$1 AND media_url IS NOT NULL`, id).Scan(&status, &name, &contentType)
	if e == pgx.ErrNoRows {
		return fail(404, "MEDIA_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if status == "expired" || status == "deleting" {
		return fail(410, "MEDIA_EXPIRED")
	}
	if status != "ready" {
		return fail(409, "MEDIA_NOT_READY")
	}
	f, e := os.Open(filepath.Join(a.cfg.MediaDir, id+".media"))
	if e != nil {
		if os.IsNotExist(e) {
			_, _ = a.pool.Exec(r.Context(), `UPDATE messages SET local_file_path=NULL,media_status='failed',media_error='MEDIA_FILE_MISSING' WHERE id=$1`, id)
			return fail(404, "MEDIA_FILE_MISSING")
		}
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return e
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	if contentType != nil {
		w.Header().Set("Content-Type", *contentType)
	}
	if name != nil {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": *name}))
	}
	http.ServeContent(w, r, id, info.ModTime(), f)
	return nil
}
func (a *App) mediaRetry(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if n, e := strconv.ParseInt(id, 10, 64); e != nil || n <= 0 || strconv.FormatInt(n, 10) != id {
		return fail(404, "MEDIA_NOT_FOUND")
	}
	tag, e := a.pool.Exec(r.Context(), `UPDATE messages SET media_status='pending',media_attempts=0,media_retry_at=now(),media_error=NULL WHERE id=$1 AND media_status IN ('failed','expired')`, id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return fail(409, "MEDIA_NOT_RETRYABLE")
	}
	respond(w, 202, map[string]any{"status": "pending"})
	return nil
}
