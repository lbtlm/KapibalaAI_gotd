package platform

import (
	"context"
	"hash/fnv"
	"time"
)

func (a *App) ids(ctx context.Context, query string) []string {
	rows, e := a.pool.Query(ctx, query)
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
