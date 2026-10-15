package platform_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestImportIdentityIsStableOnlyForThePersistedIntentAndKeepsLegacyFingerprint(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	token, _ := login(t, s, "admin")
	const sessionInput = "SQLite format 3\x00identity-parser-input-only"
	parts := []uploadPart{{"identity.session", sessionInput}, {"identity.json", `{"api_id":12345,"api_hash":"0123456789abcdef0123456789abcdef"}`}}
	upload := func() string {
		t.Helper()
		st, b := uploadAccounts(t, s, token, parts, "")
		if st != 200 {
			t.Fatal(st, b)
		}
		item := b["results"].([]any)[0].(map[string]any)
		if item["success"] != false || item["error"].(map[string]any)["code"] != "NETWORK_TIMEOUT" {
			t.Fatalf("input must fail at the absent gateway without claiming Session success: %v", item)
		}
		return item["accountId"].(string)
	}
	first := upload()
	if retry := upload(); retry != first {
		t.Fatalf("retry changed persisted identity: %s %s", first, retry)
	}
	// Reset only the unregistered test intent to exercise a new intent lifecycle.
	// No account or actual credential registration succeeded in this test.
	businessSQL(t, cfg, `DELETE FROM account_imports WHERE account_id=$1`, first)
	second := upload()
	if second == first {
		t.Errorf("new intent reused the deleted intent identity: %s", first)
	}
	oldSessionHash := sha256.Sum256([]byte(sessionInput))
	legacyID := "acct_" + hex.EncodeToString(oldSessionHash[:])[:32]
	// Exact normalized JSON used by the pre-deletion importer, with Session empty.
	legacyJSON := `{"accountId":"` + legacyID + `","session":"","apiId":12345,"apiHash":"0123456789abcdef0123456789abcdef"}`
	legacyHash := sha256.Sum256([]byte(legacyJSON))
	businessSQL(t, cfg, `UPDATE account_imports SET account_id=$2,config_fingerprint=$3 WHERE account_id=$1`, second, legacyID, hex.EncodeToString(legacyHash[:]))
	if got := upload(); got != legacyID {
		t.Fatalf("legacy import did not retain its persisted identity: %s", got)
	}
}
