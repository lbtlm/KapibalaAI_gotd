package gateway_test

import (
	"encoding/json"
	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionImportRejectsUnusableAuthKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.json")
	_ = os.WriteFile(path, []byte(`{"Version":1,"Data":{"DC":2,"Addr":"149.154.167.51:443","AuthKey":"AA==","AuthKeyID":"AA=="}}`), 0600)
	if _, err := gateway.ImportSession(path, "gotdJSON", ""); err == nil {
		t.Fatal("one-byte auth key accepted")
	}
}
func TestSessionImportPreservesSyntheticGotdSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.json")
	b, _ := json.Marshal(map[string]any{"Version": 1, "Data": map[string]any{"DC": 2, "Addr": "149.154.167.51:443", "AuthKey": make([]byte, 256), "AuthKeyID": make([]byte, 8)}})
	_ = os.WriteFile(path, b, 0600)
	result, err := gateway.ImportSession(path, "gotdJSON", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != string(b) {
		t.Fatal("session unexpectedly rewritten")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(b) {
		t.Fatal("source changed")
	}
}
