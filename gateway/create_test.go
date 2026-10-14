package gateway_test

import "testing"

func TestCreateTimeoutRecoversWithInternalKey(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "create", "code": "NETWORK_TIMEOUT", "effect": true}, 200)
	in := map[string]string{"creatorAccountId": "account-1", "idempotencyKey": "durable-create"}
	call(t, ts, "POST", "/groups", in, 504)
	first := call(t, ts, "POST", "/groups", in, 200)
	second := call(t, ts, "POST", "/groups", in, 200)
	if first["groupId"] != second["groupId"] {
		t.Fatalf("create recovery duplicated group: %v %v", first, second)
	}
}
