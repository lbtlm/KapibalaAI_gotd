package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/migration"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Config struct {
	DatabaseURL, GatewayURL, AgentURL string
	TurnTimeout                       time.Duration
}
type App struct {
	pool    *pgxpool.Pool
	cfg     Config
	schema  uint
	client  *http.Client
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	alertMu sync.Mutex
	alerts  []alert
	scope   string
	sendMu  sync.Mutex
	sending map[string]bool
	runtime *pgx.Conn
}

func ID() string { var b [16]byte; rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func New(ctx context.Context, cfg Config) (*App, error) {
	v, e := migration.Check(cfg.DatabaseURL)
	if e != nil {
		return nil, e
	}
	pc, e := pgxpool.ParseConfig(cfg.DatabaseURL)
	if e != nil {
		return nil, e
	}
	pc.MaxConns = 32
	pc.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{Name: "timestamptz", OID: pgtype.TimestamptzOID, Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC}})
		return nil
	}
	p, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		return nil, e
	}
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = "http://127.0.0.1:8081"
	}
	if cfg.AgentURL == "" {
		cfg.AgentURL = "http://127.0.0.1:8082"
	}
	if cfg.TurnTimeout == 0 {
		cfg.TurnTimeout = 12 * time.Second
	}
	var scope string
	if e = p.QueryRow(ctx, `SELECT current_schema()`).Scan(&scope); e != nil {
		p.Close()
		return nil, e
	}
	return &App{pool: p, cfg: cfg, schema: v, client: &http.Client{Timeout: 12 * time.Second}, scope: scope, sending: map[string]bool{}}, nil
}
func (a *App) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()
	a.pool.Close()
}
func (a *App) Start(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)
	if e := a.startRuntime(ctx); e != nil {
		a.cancel()
		return e
	}
	a.wg.Add(8)
	go a.watchRuntime(ctx)
	go a.eventsStream(ctx)
	go a.loop(ctx, 100*time.Millisecond, a.inboxTick)
	go a.loop(ctx, 100*time.Millisecond, a.sendTick)
	go a.loop(ctx, 100*time.Millisecond, a.sequenceTick)
	go a.loop(ctx, 100*time.Millisecond, a.agentTick)
	go a.loop(ctx, 100*time.Millisecond, a.agentBudgetTick)
	go a.loop(ctx, 100*time.Millisecond, func(ctx context.Context) { a.jobsTick(ctx); a.disconnectActions(ctx); a.flushAlerts(ctx) })
	return nil
}

type apiError struct {
	status        int
	code, message string
	extra         map[string]any
}

func (e *apiError) Error() string        { return e.code + ": " + e.message }
func fail(status int, code string) error { return &apiError{status: status, code: code, message: code} }
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, err error) {
	e := &apiError{status: 500, code: "INTERNAL_ERROR", message: "request could not be completed"}
	var known *apiError
	if errors.As(err, &known) {
		e = known
	}
	payload := map[string]any{"code": e.code, "message": e.message, "requestId": ID()}
	for k, v := range e.extra {
		payload[k] = v
	}
	respond(w, e.status, map[string]any{"error": payload})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if d.Decode(v) != nil {
		return fail(400, "VALIDATION_ERROR")
	}
	return nil
}

type handler func(http.ResponseWriter, *http.Request) error

func (a *App) wrap(fn handler, public bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !public {
			u, e := a.authorize(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if e != nil {
				failure(w, e)
				return
			}
			if r.Method != "GET" && u.role != "admin" && r.URL.Path != "/api/auth/logout" {
				failure(w, fail(403, "FORBIDDEN"))
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), authKey{}, u))
		}
		if e := fn(w, r); e != nil {
			failure(w, e)
		}
	}
}
func (a *App) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", a.wrap(func(w http.ResponseWriter, r *http.Request) error {
		respond(w, 200, map[string]any{"ok": true, "schemaVersion": a.schema})
		return nil
	}, true))
	m.HandleFunc("POST /api/auth/login", a.wrap(a.login, true))
	m.HandleFunc("POST /api/auth/refresh", a.wrap(a.refresh, true))
	m.HandleFunc("POST /api/auth/logout", a.wrap(a.logout, false))
	m.HandleFunc("GET /api/accounts", a.wrap(a.accounts, false))
	m.HandleFunc("POST /api/accounts/{id}/connect", a.wrap(a.connect, false))
	m.HandleFunc("POST /api/accounts/{id}/transition", a.wrap(a.manualTransition, false))
	m.HandleFunc("POST /api/groups", a.wrap(a.createGroup, false))
	m.HandleFunc("GET /api/groups", a.wrap(a.groups, false))
	m.HandleFunc("GET /api/groups/{id}", a.wrap(a.groupGet, false))
	m.HandleFunc("PATCH /api/groups/{id}", a.wrap(a.patchGroup, false))
	m.HandleFunc("POST /api/groups/{id}/leave-all", a.wrap(a.leaveAll, false))
	m.HandleFunc("GET /api/jobs/{id}", a.wrap(a.jobGet, false))
	m.HandleFunc("POST /api/groups/import", a.wrap(a.importGroup, false))
	m.HandleFunc("GET /ws", a.ws)
	m.HandleFunc("POST /api/groups/{id}/send", a.wrap(a.send, false))
	m.HandleFunc("GET /api/groups/{id}/messages", a.wrap(a.messages, false))
	m.HandleFunc("POST /api/sequences", a.wrap(a.createSequence, false))
	m.HandleFunc("GET /api/sequences", a.wrap(a.sequences, false))
	m.HandleFunc("POST /api/groups/{id}/sequence-runs", a.wrap(a.startSequence, false))
	m.HandleFunc("POST /api/groups/{id}/sequence-runs/preview", a.wrap(a.previewSequence, false))
	m.HandleFunc("GET /api/sequence-runs/{id}", a.wrap(a.sequenceRun, false))
	m.HandleFunc("GET /api/agent-runs/{id}", a.wrap(a.agentGet, false))
	m.HandleFunc("GET /api/groups/{id}/agent-runs", a.wrap(a.agentList, false))
	return m
}
