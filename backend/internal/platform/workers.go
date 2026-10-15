package platform

import (
	"context"
	"hash/fnv"
	"log/slog"
	"strings"
	"time"
)

// One local goroutine per durable work key; the existing database lease fences
// other processes. All workers inherit cancellation and join App.Close.
func (a *App) launchWorker(ctx context.Context, key string, fn func(context.Context) error) {
	a.workMu.Lock()
	// Reserve send capacity even when agents are waiting for send receipts.
	// At most 14 workers (including two media transfers) use lease + business connections in the 32-slot pool.
	kind, _, _ := strings.Cut(key, ":")
	limit := 4
	if kind == "job" || kind == "sequence" || kind == "media" {
		limit = 2
	}
	count := 0
	for active := range a.running {
		if strings.HasPrefix(active, kind+":") {
			count++
		}
	}
	if ctx.Err() != nil || a.running[key] || count >= limit {
		a.workMu.Unlock()
		return
	}
	a.running[key] = true
	a.workMu.Unlock()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer func() { a.workMu.Lock(); delete(a.running, key); a.workMu.Unlock() }()
		if e := a.lock(ctx, key, fn); e != nil && ctx.Err() == nil {
			slog.Warn("worker checkpoint retrying", "work", key, "error", e)
		}
	}()
}

func (a *App) ids(ctx context.Context, query string, args ...any) []string {
	rows, e := a.pool.Query(ctx, query, args...)
	if e != nil {
		return nil
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
func (a *App) lock(ctx context.Context, key string, fn func(context.Context) error) error {
	c, e := a.pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer c.Release()
	h := fnv.New64a()
	h.Write([]byte(a.scope + ":" + key))
	n := int64(h.Sum64())
	var ok bool
	if e = c.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, n).Scan(&ok); e != nil || !ok {
		return e
	}
	if _, e = c.Exec(ctx, `SELECT set_config('application_name',$1,false)`, "kapibala-lock:"+key); e != nil {
		return e
	}
	active, cancel := context.WithCancel(ctx)
	defer cancel()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-active.Done():
				return
			case <-tick.C:
				if _, e := c.Exec(active, `SELECT 1`); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	err := fn(active)
	close(stop)
	<-done
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), time.Second)
	defer cancelCleanup()
	c.Exec(cleanup, `SELECT pg_advisory_unlock($1)`, n)
	c.Exec(cleanup, `SELECT set_config('application_name','',false)`)
	return err
}
func (a *App) loop(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	defer a.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		fn(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
