package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway/internal/gateway"
)

func TestPostgresDeleteRetiresIdentityAndPreservesOtherData(t *testing.T) {
	p := isolatedStore(t)
	seed := []byte(`{"mode":"telegram","accounts":{},"groups":{},"effects":{},"events":[{"eventId":7,"type":"account_status","data":{"accountId":"old","status":"offline"}}],"nextEvent":7,"messages":[{"groupId":"historical","msgId":"1","sender":"123","text":"retained"}]}`)
	if err := p.Save(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	s, server := realServer(t, p)
	defer func() { server.Close(); s.Close() }()
	data := structuralSession(t, 71)
	input := registrationInput("old", data)
	request(t, server, "POST", "/accounts/register", input, 200)
	request(t, server, "POST", "/accounts/register", registrationInput("other", structuralSession(t, 72)), 200)
	for _, id := range []string{"old", "other"} {
		if _, err := p.DB.Exec(`INSERT INTO gateway_update_state VALUES($1,1,1,1,1,1);`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := p.DB.Exec(`INSERT INTO gateway_channel_state VALUES($1,1,1,1,1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	request(t, server, "DELETE", "/accounts/old", nil, 204)
	request(t, server, "DELETE", "/accounts/old", nil, 204)
	for _, table := range []string{"gateway_account_configs", "gateway_sessions", "gateway_update_state", "gateway_channel_state"} {
		var old, other int
		if err := p.DB.QueryRow("SELECT count(*) FILTER (WHERE account_id='old'),count(*) FILTER (WHERE account_id='other') FROM "+table).Scan(&old, &other); err != nil || old != 0 || other != 1 {
			t.Fatalf("target cleanup changed another account in %s", table)
		}
	}
	if out := request(t, server, "POST", "/accounts/register", input, 409); out["code"] != "ACCOUNT_RETIRED" {
		t.Fatal("deleted identity accepted registration")
	}
	if out := request(t, server, "POST", "/accounts/old/connect", nil, 409); out["code"] != "ACCOUNT_RETIRED" {
		t.Fatal("deleted identity lost its retired connection fence")
	}
	request(t, server, "POST", "/accounts/register", registrationInput("new", data), 200)
	request(t, server, "DELETE", "/accounts/old", nil, 204)
	if out := request(t, server, "GET", "/accounts/new/registration", nil, 200); out["status"] != "ready" {
		t.Fatal("old delete touched the reuploaded identity")
	}
	server.Close()
	s.Close()
	var err error
	p, err = gateway.OpenPostgres(context.Background(), testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	s, server = realServer(t, p)
	request(t, server, "DELETE", "/accounts/never-existed", nil, 204)
	if out := request(t, server, "POST", "/accounts/register", input, 409); out["code"] != "ACCOUNT_RETIRED" {
		t.Fatal("retirement did not survive reconstruction")
	}
	b, err := p.Load(context.Background())
	var snapshot struct {
		Accounts  map[string]json.RawMessage `json:"accounts"`
		Events    []json.RawMessage          `json:"events"`
		Messages  []json.RawMessage          `json:"messages"`
		NextEvent int                        `json:"nextEvent"`
	}
	if err != nil || json.Unmarshal(b, &snapshot) != nil || snapshot.Accounts["old"] != nil || snapshot.Accounts["new"] == nil || snapshot.NextEvent != 7 || len(snapshot.Events) != 1 || len(snapshot.Messages) != 1 {
		t.Fatal("delete failed to preserve historical events/messages or remove its snapshot")
	}
}

func TestPostgresDeleteCancellationLeavesOtherAccountsAndServiceAvailable(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer func() { server.Close(); s.Close() }()
	request(t, server, "POST", "/accounts/register", registrationInput("cancelled", structuralSession(t, 75)), 200)
	request(t, server, "POST", "/accounts/register", registrationInput("unrelated", structuralSession(t, 76)), 200)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("DELETE", "/accounts/cancelled", nil).WithContext(ctx)
	s.ServeHTTP(httptest.NewRecorder(), r)
	request(t, server, "GET", "/health", nil, 200)
	for _, id := range []string{"cancelled", "unrelated"} {
		if out := request(t, server, "GET", "/accounts/"+id+"/registration", nil, 200); out["status"] != "ready" {
			t.Fatal("cancelled request altered credentials")
		}
	}
	var n int
	if err := p.DB.QueryRow("SELECT count(*) FROM gateway_retired_accounts").Scan(&n); err != nil || n != 0 {
		t.Fatal("cancelled request retired an identity")
	}
}

func TestPostgresRestartEndsOrphanedNonOnlineConnectionIntent(t *testing.T) {
	for _, status := range []string{"idle", "offline"} {
		t.Run(status, func(t *testing.T) {
			p := isolatedStore(t)
			s, server := realServer(t, p)
			request(t, server, "POST", "/accounts/register", registrationInput("intent", structuralSession(t, 80)), 200)
			server.Close()
			s.Close()
			var err error
			p, err = gateway.OpenPostgres(context.Background(), testDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			b, err := p.Load(context.Background())
			var snapshot map[string]any
			if err != nil || json.Unmarshal(b, &snapshot) != nil {
				t.Fatal("snapshot unavailable")
			}
			snapshot["accounts"] = map[string]any{"intent": map[string]any{"status": status, "connectionId": "persisted-before-adapter"}}
			b, _ = json.Marshal(snapshot)
			if err = p.Save(context.Background(), b); err != nil {
				t.Fatal(err)
			}
			// Neither state is resumed. After process reconstruction no old Run
			// exists, so this orphaned intent must end without connecting.
			s, server = realServer(t, p)
			defer func() { server.Close(); s.Close() }()
			b, err = p.Load(context.Background())
			var resumed struct {
				Accounts map[string]struct {
					Status       string          `json:"status"`
					ConnectionID string          `json:"connectionId"`
					Ended        map[string]bool `json:"endedConnections"`
				} `json:"accounts"`
				Events []json.RawMessage `json:"events"`
			}
			if err != nil || json.Unmarshal(b, &resumed) != nil || resumed.Accounts["intent"].Status != status || resumed.Accounts["intent"].ConnectionID != "" || !resumed.Accounts["intent"].Ended["persisted-before-adapter"] || len(resumed.Events) != 1 || !bytes.Contains(resumed.Events[0], []byte(`"connectionId":"persisted-before-adapter"`)) {
				t.Fatal("restart failed to preserve status and reconcile its orphaned CID through one offline event")
			}
			if out := request(t, server, "POST", "/accounts/intent/connect", map[string]string{"connectionId": "persisted-before-adapter"}, 409); out["code"] != "CONNECTION_ENDED" {
				t.Fatal("restart allowed the old connection generation to revive")
			}
			request(t, server, "DELETE", "/accounts/intent", nil, 204)
		})
	}
}

func TestPostgresConnectFailureEndsIntentWithoutMaskingOriginalError(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer func() { server.Close(); s.Close() }()
	request(t, server, "POST", "/accounts/register", registrationInput("no-session", structuralSession(t, 81)), 200)
	// Missing local storage fails before constructing a Telegram client or Run.
	if _, err := p.DB.Exec("DELETE FROM gateway_sessions WHERE account_id='no-session'"); err != nil {
		t.Fatal(err)
	}
	out := request(t, server, "POST", "/accounts/no-session/connect", map[string]string{"connectionId": "failed-start"}, 503)
	if out["code"] != "CREDENTIALS_INCOMPLETE" {
		t.Fatal("connection cleanup replaced the concrete startup error")
	}
	b, err := p.Load(context.Background())
	var snapshot struct {
		Accounts map[string]struct {
			Status       string          `json:"status"`
			ConnectionID string          `json:"connectionId"`
			Ended        map[string]bool `json:"endedConnections"`
		} `json:"accounts"`
	}
	if err != nil || json.Unmarshal(b, &snapshot) != nil || snapshot.Accounts["no-session"].ConnectionID != "" || !snapshot.Accounts["no-session"].Ended["failed-start"] || snapshot.Accounts["no-session"].Status != "idle" {
		t.Fatal("known failed startup left a permanent connection intent")
	}
	if out := request(t, server, "POST", "/accounts/no-session/connect", map[string]string{"connectionId": "failed-start"}, 409); out["code"] != "CONNECTION_ENDED" {
		t.Fatal("ended connection ID could be reused")
	}
	request(t, server, "DELETE", "/accounts/no-session", nil, 204)
}

func TestPostgresDeleteOnlyUnlinksConfirmedHistoricalLeave(t *testing.T) {
	for _, left := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown-or-terminal", true: "confirmed-left"}[left], func(t *testing.T) {
			p := isolatedStore(t)
			s, server := realServer(t, p)
			request(t, server, "POST", "/accounts/register", registrationInput("historical", structuralSession(t, 77)), 200)
			server.Close()
			s.Close()
			var err error
			p, err = gateway.OpenPostgres(context.Background(), testDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			b, err := p.Load(context.Background())
			var snapshot map[string]any
			if err != nil || json.Unmarshal(b, &snapshot) != nil {
				t.Fatal("snapshot unavailable")
			}
			snapshot["accounts"] = map[string]any{"historical": map[string]any{"status": "session_expired", "platformId": "123"}}
			snapshot["groups"] = map[string]any{"retained": map[string]any{"owner": "historical", "members": map[string]bool{"456": true}, "admins": map[string]bool{"historical": true}, "accessHashes": map[string]int64{"historical": 1}, "memberRoles": map[string]string{"123": "creator", "456": "member"}, "historyThrough": map[string]int{"historical": 4}, "leftAccounts": map[string]bool{"historical": left}, "recovery": map[string]any{"accountId": "historical", "status": "recovered_with_limits", "throughId": 4}}}
			b, _ = json.Marshal(snapshot)
			if err = p.Save(context.Background(), b); err != nil {
				t.Fatal(err)
			}
			s, server = realServer(t, p)
			defer func() { server.Close(); s.Close() }()
			if !left {
				if out := request(t, server, "DELETE", "/accounts/historical", nil, 409); out["code"] != "ACCOUNT_IN_USE" || out["reason"] != "managed_group" {
					t.Fatal("unknown membership lost conservative deletion guard")
				}
				return
			}
			request(t, server, "DELETE", "/accounts/historical", nil, 204)
			b, err = p.Load(context.Background())
			if err != nil || json.Unmarshal(b, &snapshot) != nil {
				t.Fatal("snapshot unavailable after delete")
			}
			g := snapshot["groups"].(map[string]any)["retained"].(map[string]any)
			if g["owner"] != "" || len(g["admins"].(map[string]any)) != 0 || len(g["accessHashes"].(map[string]any)) != 0 || g["memberRoles"].(map[string]any)["123"] != nil || g["memberRoles"].(map[string]any)["456"] != "member" || g["recovery"].(map[string]any)["throughId"] != float64(4) || g["recovery"].(map[string]any)["accountId"] != "" {
				t.Fatal("historical leave cleanup destroyed history or left credential references")
			}
		})
	}
}

func TestPostgresDeleteRollsBackEveryResourceOnStorageFailure(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer func() { server.Close(); s.Close() }()
	request(t, server, "POST", "/accounts/register", registrationInput("rollback", structuralSession(t, 73)), 200)
	_, err := p.DB.Exec(`CREATE FUNCTION gateway_test_delete_reject() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''controlled delete rejection''; END'; CREATE TRIGGER gateway_test_delete_reject BEFORE DELETE ON gateway_sessions FOR EACH ROW EXECUTE FUNCTION gateway_test_delete_reject()`)
	if err != nil {
		t.Fatal(err)
	}
	defer p.DB.Exec(`DROP TRIGGER IF EXISTS gateway_test_delete_reject ON gateway_sessions; DROP FUNCTION IF EXISTS gateway_test_delete_reject()`)
	request(t, server, "DELETE", "/accounts/rollback", nil, 503)
	select {
	case <-s.Done():
		t.Fatal("confirmed rollback cancelled unrelated account work")
	default:
	}
	for _, table := range []string{"gateway_sessions", "gateway_account_configs"} {
		var n int
		if err := p.DB.QueryRow("SELECT count(*) FROM " + table + " WHERE account_id='rollback'").Scan(&n); err != nil || n != 1 {
			t.Fatal("failed delete partially removed credentials")
		}
	}
	var retired bool
	var stored []byte
	if err := p.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM gateway_retired_accounts WHERE account_id='rollback')").Scan(&retired); err != nil || retired {
		t.Fatal("rolled-back delete retired its identity")
	}
	if err := p.DB.QueryRow("SELECT data FROM gateway_state WHERE id=1").Scan(&stored); err != nil || !bytes.Contains(stored, []byte(`"rollback"`)) {
		t.Fatal("rolled-back delete removed its snapshot")
	}
}

func TestPostgresConcurrentRegisterAndDeleteCannotResurrectRetiredIdentity(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer func() { server.Close(); s.Close() }()
	input := registrationInput("interleaved", structuralSession(t, 74))
	body, _ := json.Marshal(input)
	var wg sync.WaitGroup
	results := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			method, path := "POST", "/accounts/register"
			if i%2 == 1 {
				method, path = "DELETE", "/accounts/interleaved"
			}
			r, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
			response, err := server.Client().Do(r)
			if err != nil {
				results <- 0
				return
			}
			response.Body.Close()
			results <- response.StatusCode
		}(i)
	}
	wg.Wait()
	close(results)
	for status := range results {
		if status != 200 && status != 204 && status != 409 && status != 503 {
			t.Fatalf("unexpected concurrent response %d", status)
		}
	}
	request(t, server, "DELETE", "/accounts/interleaved", nil, 204)
	if out := request(t, server, "POST", "/accounts/register", input, 409); out["code"] != "ACCOUNT_RETIRED" {
		t.Fatal("interleaved registration escaped retirement")
	}
	request(t, server, "GET", "/health", nil, 200)
	if out := request(t, server, "GET", "/accounts/interleaved/registration", nil, 200); out["status"] != "absent" {
		t.Fatal("late register recreated a retired snapshot")
	}
}
