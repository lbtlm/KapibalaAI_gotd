package platform

import (
	"context"
)

type alert struct{ Kind, Ref, Message string }

func (a *App) queueAlert(kind, ref, message string) {
	a.alertMu.Lock()
	defer a.alertMu.Unlock()
	if len(a.alerts) < 100 {
		a.alerts = append(a.alerts, alert{kind, ref, message})
	}
}
func (a *App) flushAlerts(ctx context.Context) {
	a.alertMu.Lock()
	pending := a.alerts
	a.alerts = nil
	a.alertMu.Unlock()
	for i, v := range pending {
		tx, e := a.pool.Begin(ctx)
		if e == nil {
			e = emit(ctx, tx, "inconsistency", map[string]any{"kind": v.Kind, "ref": v.Ref, "message": v.Message})
			if e == nil {
				e = tx.Commit(ctx)
			} else {
				tx.Rollback(ctx)
			}
		}
		if e != nil {
			a.alertMu.Lock()
			a.alerts = append(pending[i:], a.alerts...)
			a.alertMu.Unlock()
			return
		}
	}
}
