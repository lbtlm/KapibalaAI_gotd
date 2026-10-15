package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func init() {
	if os.Getenv("KAPIBALA_DTO_NONUTC") == "true" {
		time.Local = time.FixedZone("test-non-UTC", 5*3600+45*60)
	}
}

func TestEmptyFinalSummaryRemainsStringRatherThanNull(t *testing.T) {
	for _, final := range []string{"end_turn", "finish"} {
		t.Run(final, func(t *testing.T) {
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if final == "finish" {
					fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"finish","name":"finish","input":{"summary":""}}]}`)
				} else {
					fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":""}]}`)
				}
			}))
			defer agent.Close()
			a, s, cfg := fixtureConfig(t, platform.Config{AgentURL: agent.URL})
			group := insertAgentInput(t, cfg)
			a.Start(context.Background())
			token, _ := login(t, s, "admin")

			eventually(t, 4*time.Second, func() bool {
				runs := runList(t, s, token, group)
				if len(runs) != 1 || runs[0]["status"] != "finished" {
					return false
				}
				_, detail, _ := call(t, s, "GET", "/api/agent-runs/"+runs[0]["id"].(string), token, "")
				if detail["summary"] != "" || runs[0]["summary"] != "" {
					t.Fatalf("legal empty final summary lost: detail=%v list=%v", detail["summary"], runs[0]["summary"])
				}
				return true
			})
		})
	}
}

func TestPublicTimesUseUTCWhenHostTimezoneIsNotUTC(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("real PostgreSQL TEST_DATABASE_URL required")
	}
	if os.Getenv("KAPIBALA_DTO_NONUTC") != "true" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublicTimesUseUTCWhenHostTimezoneIsNotUTC$", "-test.timeout=25s")
		cmd.Env = append(os.Environ(), "KAPIBALA_DTO_NONUTC=true")
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("non-UTC subprocess: %v %s", e, out)
		}
		return
	}
	_, s, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	token, _ := login(t, s, "admin")
	businessSQL(t, cfg, `UPDATE accounts SET status='rate_limited',rate_limited_until=now()+interval '1 hour' WHERE id='account-1'`)
	businessSQL(t, cfg, `INSERT INTO messages(group_id,msg_id,sender_platform_user_id,is_own,text) VALUES($1,'utc-input','database-author',false,'database timestamp input')`, group)
	req, _ := http.NewRequest("GET", s.URL+"/api/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := s.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var accounts []map[string]any
	json.NewDecoder(res.Body).Decode(&accounts)
	for _, account := range accounts {
		if account["rateLimitedUntil"] != nil && !strings.HasSuffix(account["rateLimitedUntil"].(string), "Z") {
			t.Errorf("account time is not UTC: %v", account["rateLimitedUntil"])
		}
	}
	_, messages, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
	for _, item := range messages["items"].([]any) {
		if sent := item.(map[string]any)["sentAt"].(string); !strings.HasSuffix(sent, "Z") {
			t.Errorf("message time is not UTC: %s", sent)
		}
	}
	_, sequence, _ := call(t, s, "POST", "/api/sequences", token, `{"name":"UTC schedule","steps":[{"index":1,"accountRole":"member","text":"later","delaySeconds":30}]}`)
	_, run, _ := call(t, s, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"`+sequence["id"].(string)+`"}`)
	_, detail, _ := call(t, s, "GET", "/api/sequence-runs/"+run["runId"].(string), token, "")
	if scheduled := detail["steps"].([]any)[0].(map[string]any)["scheduledAt"].(string); !strings.HasSuffix(scheduled, "Z") {
		t.Errorf("sequence time is not UTC: %s", scheduled)
	}
}
