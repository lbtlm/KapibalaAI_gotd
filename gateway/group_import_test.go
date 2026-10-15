package gateway_test

import "testing"

func TestGroupImportByIDRequiresManagedOnlineAccount(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer s.Close()
	defer server.Close()
	// A structurally valid snapshot ID must reach account validation. No account
	// or Telegram success is simulated in this test.
	request(t, server, "POST", "/groups/import", map[string]any{"groupId": "tg-123", "accountIds": []string{"missing"}}, 404)
	request(t, server, "POST", "/groups/import", map[string]any{"groupId": "../123", "accountIds": []string{"missing"}}, 400)
}
