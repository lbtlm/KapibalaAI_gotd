package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"time"
)

type authKey struct{}
type identity struct{ session, role, username string }

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (a *App) authorize(ctx context.Context, token string) (identity, error) {
	var u identity
	e := a.pool.QueryRow(ctx, `SELECT s.id,u.role,u.username FROM access_tokens t JOIN sessions s ON s.id=t.session_id JOIN users u ON u.username=s.username WHERE t.hash=$1 AND t.expires_at>now() AND NOT s.revoked`, hash(token)).Scan(&u.session, &u.role, &u.username)
	if e == pgx.ErrNoRows {
		return u, fail(401, "UNAUTHORIZED")
	}
	if e != nil {
		return u, fail(503, "DATABASE_UNAVAILABLE")
	}
	return u, nil
}
func cookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: token, Path: "/api/auth", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600})
}
func issue(ctx context.Context, tx pgx.Tx, session string) (string, string, error) {
	access, refresh := ID()+ID(), ID()+ID()
	_, e := tx.Exec(ctx, `INSERT INTO access_tokens(hash,session_id,expires_at) VALUES($1,$2,now()+interval '15 minutes')`, hash(access), session)
	if e != nil {
		return "", "", e
	}
	_, e = tx.Exec(ctx, `INSERT INTO refresh_tokens(hash,session_id,expires_at) VALUES($1,$2,now()+interval '30 days')`, hash(refresh), session)
	return access, refresh, e
}
func (a *App) login(w http.ResponseWriter, r *http.Request) error {
	var b struct{ Username, Password string }
	if e := decode(r, &b); e != nil {
		return e
	}
	var digest, role string
	e := a.pool.QueryRow(r.Context(), `SELECT password_hash,role FROM users WHERE username=$1`, b.Username).Scan(&digest, &role)
	if e != nil || bcrypt.CompareHashAndPassword([]byte(digest), []byte(b.Password)) != nil {
		return fail(401, "UNAUTHORIZED")
	}
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	session := ID()
	_, e = tx.Exec(ctx, `INSERT INTO sessions(id,username) VALUES($1,$2)`, session, b.Username)
	if e != nil {
		return e
	}
	access, refresh, e := issue(ctx, tx, session)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	cookie(w, r, refresh)
	respond(w, 200, map[string]any{"accessToken": access, "role": role})
	return nil
}
func (a *App) refresh(w http.ResponseWriter, r *http.Request) error {
	c, e := r.Cookie("refresh_token")
	if e != nil {
		return fail(401, "UNAUTHORIZED")
	}
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var session, role string
	var used, revoked bool
	var expires time.Time
	e = tx.QueryRow(ctx, `SELECT s.id,t.used,s.revoked,t.expires_at,u.role FROM refresh_tokens t JOIN sessions s ON s.id=t.session_id JOIN users u ON u.username=s.username WHERE t.hash=$1 FOR UPDATE OF s,t`, hash(c.Value)).Scan(&session, &used, &revoked, &expires, &role)
	if e != nil {
		return fail(401, "UNAUTHORIZED")
	}
	if used {
		if _, e = tx.Exec(ctx, `UPDATE sessions SET revoked=true WHERE id=$1`, session); e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		return fail(401, "UNAUTHORIZED")
	}
	if revoked || time.Now().After(expires) {
		return fail(401, "UNAUTHORIZED")
	}
	if _, e = tx.Exec(ctx, `UPDATE refresh_tokens SET used=true WHERE hash=$1`, hash(c.Value)); e != nil {
		return e
	}
	access, refresh, e := issue(ctx, tx, session)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	cookie(w, r, refresh)
	respond(w, 200, map[string]any{"accessToken": access, "role": role})
	return nil
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) error {
	u := r.Context().Value(authKey{}).(identity)
	_, e := a.pool.Exec(r.Context(), `UPDATE sessions SET revoked=true WHERE id=$1`, u.session)
	if e != nil {
		return e
	}
	cookie(w, r, "")
	respond(w, 200, map[string]any{})
	return nil
}
