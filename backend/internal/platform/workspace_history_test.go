package platform_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSequenceBatchHistoryPaginatesWithinGroup(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	businessSQL(t, cfg, `INSERT INTO sequences(id,name,steps) VALUES('history-script','History script','[]'); INSERT INTO groups(id) VALUES('other-group'); INSERT INTO sequence_runs(id,group_id,sequence_id,status,created_at) SELECT 'batch-'||n,'database-group','history-script','finished','2026-01-01'::timestamptz+make_interval(secs=>n) FROM generate_series(1,3)n; INSERT INTO sequence_runs(id,group_id,sequence_id,status) VALUES('other-batch','other-group','history-script','finished')`)
	token, _ := login(t, s, "viewer")
	status, page, _ := call(t, s, "GET", "/api/groups/"+group+"/sequence-runs?limit=2", token, "")
	if status != 200 {
		t.Fatalf("history status=%d", status)
	}
	items := page["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != "batch-3" || page["nextCursor"] != "batch-2" {
		t.Fatalf("history=%v", page)
	}
	status, page, _ = call(t, s, "GET", "/api/groups/"+group+"/sequence-runs?limit=2&before=batch-2", token, "")
	if status != 200 || len(page["items"].([]any)) != 1 || page["nextCursor"] != nil {
		t.Fatalf("second page=%d %v", status, page)
	}
	status, _, _ = call(t, s, "GET", "/api/groups/"+group+"/sequence-runs?before=other-batch", token, "")
	if status != 400 {
		t.Fatalf("cross-group cursor status=%d", status)
	}
}

func TestAgentDetailReturnsStoredToolResultBeyondSummary(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	full := `{"messages":[{"text":"` + strings.Repeat("上下文", 120) + `末尾证据"}],"truncated":false}`
	history, _ := json.Marshal([]any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "read-1", "content": full}}}})
	businessSQL(t, cfg, `INSERT INTO agent_runs(id,group_id,status,history,steps) VALUES('history-run',$1,'finished',$2,'[{"kind":"tool_use","toolUseId":"read-1","name":"get_recent_messages","resultSummary":"short summary"}]')`, group, history)
	token, _ := login(t, s, "viewer")
	status, run, _ := call(t, s, "GET", "/api/agent-runs/history-run", token, "")
	if status != 200 {
		t.Fatalf("detail=%d", status)
	}
	step := run["steps"].([]any)[0].(map[string]any)
	if step["result"] != full || step["resultSummary"] != "short summary" {
		t.Fatalf("full result missing or summary changed")
	}
	if _, ok := run["history"]; ok {
		t.Fatal("private model history leaked")
	}
}
