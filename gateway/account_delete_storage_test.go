package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram/updates"
)

func TestPostgresDeleteRejectsPersistedIntentBeforeAnyClientSlot(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.Contains(parsed.Path, "inspection") && !strings.Contains(parsed.Path, "test") {
		t.Fatal("isolated inspection/test database required")
	}
	ctx := context.Background()
	p, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.DB.Exec("TRUNCATE gateway_state,gateway_account_configs,gateway_sessions,gateway_update_state,gateway_channel_state,gateway_retired_accounts"); err != nil {
		p.Close()
		t.Fatal(err)
	}
	a, err := NewTelegramAdapter(ctx, p.DB)
	if err != nil {
		p.Close()
		t.Fatal(err)
	}
	s, err := New(ctx, Config{Store: p, Telegram: a})
	if err != nil {
		a.Close()
		p.Close()
		t.Fatal(err)
	}
	defer s.Close()
	for _, status := range []string{"idle", "offline"} {
		for _, locked := range []bool{false, true} {
			// Persist the exact domain input at Connect's unlock boundary. No
			// credentials/client/RPC result is supplied or reported as successful.
			s.mu.Lock()
			s.state.Accounts["intent"] = &account{Status: status, ConnectionID: "before-client-slot"}
			err = s.save(ctx)
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if locked {
				a.mu.Lock()
			}
			recorder := httptest.NewRecorder()
			s.ServeHTTP(recorder, httptest.NewRequest("DELETE", "/accounts/intent", nil))
			if locked {
				a.mu.Unlock()
			}
			var out map[string]any
			if json.Unmarshal(recorder.Body.Bytes(), &out) != nil || recorder.Code != 409 || out["code"] != "ACCOUNT_IN_USE" || out["reason"] != "active_connection" {
				t.Fatalf("status %s mutex-held=%t: persisted intent returned %d instead of409", status, locked, recorder.Code)
			}
		}
	}
	var n int
	if err = p.DB.QueryRow("SELECT count(*) FROM gateway_retired_accounts").Scan(&n); err != nil || n != 0 {
		t.Fatal("intent was retired before its client could start")
	}
}

// These are durable SDK storage inputs, never a client or RPC substitute.
func TestPostgresDeletedAccountRejectsLateSDKStorageWrites(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.Contains(parsed.Path, "inspection") && !strings.Contains(parsed.Path, "test") {
		t.Fatal("isolated inspection/test database required")
	}
	ctx := context.Background()
	p, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err = p.DB.Exec("TRUNCATE gateway_state,gateway_account_configs,gateway_sessions,gateway_update_state,gateway_channel_state,gateway_retired_accounts"); err != nil {
		t.Fatal(err)
	}
	var key crypto.Key
	copy(key[:], bytes.Repeat([]byte{79}, 256))
	auth := key.WithID()
	memory := &session.StorageMemory{}
	if err = (&session.Loader{Storage: memory}).Save(ctx, &session.Data{DC: 2, Addr: "192.0.2.1:443", AuthKey: auth.Value[:], AuthKeyID: auth.ID[:]}); err != nil {
		t.Fatal(err)
	}
	data, _ := memory.Bytes(nil)
	fingerprint, err := authorizationFingerprint(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	c := AccountConfig{ID: "storage-only", APIID: 123, APIHash: "0123456789abcdef0123456789abcdef"}
	if err = p.RegisterAccount(ctx, c, data, registration{fingerprint, configFingerprint(c)}); err != nil {
		t.Fatal(err)
	}
	storage := dbSession{p.DB, c.ID}
	stateStorage := updateStorage{db: p.DB, id: c.ID, owner: p}
	if err = storage.StoreSession(ctx, data); err != nil {
		t.Fatal(err)
	}
	if err = stateStorage.SetState(ctx, 1, updates.State{Pts: 1}); err != nil {
		t.Fatal(err)
	}
	if err = stateStorage.SetChannelPts(ctx, 1, 2, 3); err != nil {
		t.Fatal(err)
	}
	if err = stateStorage.SetChannelAccessHash(ctx, 1, 2, 4); err != nil {
		t.Fatal(err)
	}
	next := initialState()
	next.Mode = "telegram"
	snapshot, _ := json.Marshal(next)
	if err = p.DeleteAccount(ctx, c.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = storage.StoreSession(ctx, data); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("late Session writer recreated deleted credentials")
	}
	for _, write := range []func() error{
		func() error { return stateStorage.SetState(ctx, 1, updates.State{Pts: 2}) },
		func() error { return stateStorage.SetChannelPts(ctx, 1, 2, 5) },
		func() error { return stateStorage.SetChannelAccessHash(ctx, 1, 2, 6) },
	} {
		if err = write(); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("late SDK state writer escaped retirement")
		}
	}
	for _, table := range []string{"gateway_sessions", "gateway_update_state", "gateway_channel_state"} {
		var n int
		if err = p.DB.QueryRow("SELECT count(*) FROM "+table+" WHERE account_id=$1", c.ID).Scan(&n); err != nil || n != 0 {
			t.Fatal("late SDK state survived account deletion")
		}
	}
}
