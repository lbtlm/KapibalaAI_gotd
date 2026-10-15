package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestSecondPromoteAttemptRecoversAcrossBothCrashWindows(t *testing.T) {
	for _, window := range []string{"prepared_before_rpc", "succeeded_before_checkpoint"} {
		t.Run(window, func(t *testing.T) {
			g := gatewayFixture(t)
			base := g.server.Config.Handler
			var attempts atomic.Int32
			var admin atomic.Bool
			first, second, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(release)
			g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/groups/gateway-group/promote" {
					var b map[string]string
					json.NewDecoder(r.Body).Decode(&b)
					if attempts.Add(1) == 1 {
						close(first)
						w.WriteHeader(409)
						fmt.Fprint(w, `{"code":"NOT_MEMBER_YET"}`)
						return
					}
					admin.Store(true)
					if window == "succeeded_before_checkpoint" {
						close(second)
						select {
						case <-release:
						case <-r.Context().Done():
						}
					}
					fmt.Fprint(w, `{}`)
					return
				}
				if r.URL.Path == "/groups/gateway-group/members" {
					role := "member"
					if admin.Load() {
						role = "admin"
					}
					fmt.Fprintf(w, `[{"platformUserId":"p-account-1","role":"creator"},{"platformUserId":"p-account-2","role":%q},{"platformUserId":"p-account-3","role":"member"}]`, role)
					return
				}
				base.ServeHTTP(w, r)
			})
			a, _, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
			a.Close()
			cmd, s := childServer(t, cfg)
			alive := true
			defer func() {
				if alive {
					cmd.Process.Kill()
					cmd.Wait()
				}
			}()
			token, _ := login(t, s, "admin")
			for _, id := range []string{"account-1", "account-2", "account-3"} {
				call(t, s, "POST", "/api/accounts/"+id+"/connect", token, "{}")
			}
			_, out, _ := call(t, s, "POST", "/api/groups", token, `{"creatorAccountId":"account-1","memberAccountIds":["account-2","account-3"]}`)
			job := out["jobId"].(string)
			select {
			case <-first:
			case <-time.After(8 * time.Second):
				t.Fatal("first promote not reached")
			}
			pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			if window == "prepared_before_rpc" {
				tx, e := pool.Begin(context.Background())
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback(context.Background())
				if _, e = tx.Exec(context.Background(), `LOCK TABLE groups IN ACCESS EXCLUSIVE MODE`); e != nil {
					t.Fatal(e)
				}
				// Infrastructure barrier only: the next group read is blocked after
				// the job's attempt checkpoint commits, before the gateway receives it.
				eventually(t, 3*time.Second, func() bool {
					var attempt int
					pool.QueryRow(context.Background(), `SELECT (state->>'promoteAttempts')::int FROM jobs WHERE id=$1`, job).Scan(&attempt)
					return attempt == 2
				})
				cmd.Process.Kill()
				cmd.Wait()
				alive = false
				tx.Rollback(context.Background())
			} else {
				select {
				case <-second:
				case <-time.After(4 * time.Second):
					t.Fatal("second promote not reached")
				}
				cmd.Process.Kill()
				cmd.Wait()
				alive = false
			}
			restarted, server := childServer(t, cfg)
			defer func() { restarted.Process.Kill(); restarted.Wait() }()
			var group string
			eventually(t, 4*time.Second, func() bool {
				_, jobState, _ := call(t, server, "GET", "/api/jobs/"+job, token, "")
				if jobState["status"] == "failed" {
					t.Fatalf("recoverable second attempt falsely failed: %v", jobState)
				}
				group, _ = jobState["groupId"].(string)
				return jobState["status"] == "finished"
			})
			_, detail, _ := call(t, server, "GET", "/api/groups/"+group, token, "")
			for _, m := range detail["members"].([]any) {
				member := m.(map[string]any)
				if member["accountId"] == "account-2" && member["role"] != "admin" {
					t.Fatalf("recovered job lost admin role: %v", member)
				}
			}
			if attempts.Load() != 2 {
				t.Fatalf("promote exceeded two logical attempts: %d", attempts.Load())
			}
		})
	}
}
