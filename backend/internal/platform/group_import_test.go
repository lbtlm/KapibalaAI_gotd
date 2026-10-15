package platform_test

import "testing"

func TestGroupImportValidatesAccountSnapshotBeforeGateway(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	token, _ := login(t, s, "admin")
	businessSQL(t, cfg, `INSERT INTO accounts(id,status,platform_user_id) VALUES('import-a','online','123'),('offline-a','idle',NULL)`)
	businessSQL(t, cfg, `INSERT INTO account_group_snapshots(account_id,items,last_synced_at) VALUES('import-a','[{"id":"chat-1","title":"普通群","type":"group","role":"creator"},{"id":"tg-1","title":"超级群","type":"supergroup","role":"admin"}]',now())`)
	for _, tc := range []struct {
		body, code string
		status     int
	}{
		{`{"gatewayGroupId":"tg-1","accountIds":["offline-a"]}`, "ACCOUNT_NOT_ONLINE", 422},
		{`{"gatewayGroupId":"tg-1","accountIds":["import-a","import-a"]}`, "VALIDATION_ERROR", 400},
		{`{"gatewayGroupId":"chat-1","accountIds":["import-a"]}`, "GROUP_TYPE_UNSUPPORTED", 422},
		{`{"gatewayGroupId":"tg-2","accountIds":["import-a"]}`, "GROUP_SNAPSHOT_REQUIRED", 409},
	} {
		st, b, _ := call(t, s, "POST", "/api/groups/import", token, tc.body)
		if st != tc.status || b["error"].(map[string]any)["code"] != tc.code {
			t.Fatalf("expected %d %s got %d %v", tc.status, tc.code, st, b)
		}
	}
	// Valid cached metadata is not evidence that the gateway imported the group.
	st, b, _ := call(t, s, "POST", "/api/groups/import", token, `{"gatewayGroupId":"tg-1","accountIds":["import-a"]}`)
	if st != 503 || b["error"].(map[string]any)["code"] != "GATEWAY_UNAVAILABLE" {
		t.Fatal(st, b)
	}
}
