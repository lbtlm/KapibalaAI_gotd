package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func readJSON(t *testing.T, sURL, path, token string, out any) int {
	t.Helper()
	req, _ := http.NewRequest("GET", sURL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if e = json.NewDecoder(res.Body).Decode(out); e != nil {
		t.Fatal(e)
	}
	return res.StatusCode
}

func TestImportReconcilesGatewaySuccessAfterBackendSaveFailure(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	registered := map[string]bool{}
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		id := b["accountId"].(string)
		mu.Lock()
		registered[id] = true
		mu.Unlock()
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"accountId": id, "status": "idle"})
	}))
	defer g.Close()
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.URL})
	token, _ := login(t, s, "admin")
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	// Infrastructure failure at the local checkpoint, after the gateway accepted.
	_, e = pool.Exec(context.Background(), `CREATE FUNCTION reject_import_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id LIKE 'acct_%' THEN RAISE EXCEPTION 'checkpoint unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_import_checkpoint BEFORE INSERT ON accounts FOR EACH ROW EXECUTE FUNCTION reject_import_checkpoint()`)
	if e != nil {
		t.Fatal(e)
	}
	parts := []uploadPart{{"recover.session", "SQLite format 3\x00recover"}, {"recover.json", `{"api_id":12345,"api_hash":"0123456789abcdef0123456789abcdef"}`}}
	st, b := uploadAccounts(t, s, token, parts, "")
	if st != 200 || b["results"].([]any)[0].(map[string]any)["success"] != false || calls.Load() != 1 {
		t.Fatalf("checkpoint fault not reached: %d %v", st, b)
	}
	if _, e = pool.Exec(context.Background(), `DROP TRIGGER reject_import_checkpoint ON accounts; DROP FUNCTION reject_import_checkpoint()`); e != nil {
		t.Fatal(e)
	}
	a.Close()
	reopened, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	restored := httptest.NewServer(reopened.Handler())
	defer restored.Close()
	st, b = uploadAccounts(t, restored, token, parts, "")
	if st != 200 || b["results"].([]any)[0].(map[string]any)["success"] != true || calls.Load() != 2 {
		t.Fatalf("pending import failed to recover: %d %v", st, b)
	}
	mu.Lock()
	effects := len(registered)
	mu.Unlock()
	if effects != 1 {
		t.Fatalf("new identity created on retry: %d", effects)
	}
}

func TestAccountLogsStablePagesFiltersAndSafeErrors(t *testing.T) {
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		fmt.Fprint(w, `{"code":"API_HASH_0123456789abcdef0123456789abcdef","private":"SQLite-secret"}`)
	}))
	defer g.Close()
	_, s := fixture(t, platform.Config{GatewayURL: g.URL})
	token, _ := login(t, s, "admin")
	for i := 0; i < 23; i++ {
		st, b, _ := call(t, s, "POST", "/api/account-groups/move", token, `{"accountIds":["account-1"],"groupId":null}`)
		if st != 200 {
			t.Fatal(st, b)
		}
	}
	st, b, _ := call(t, s, "GET", "/api/accounts/account-1/logs", token, "")
	if st != 200 || len(b["items"].([]any)) != 20 || b["nextCursor"] == nil {
		t.Fatalf("first page: %d %v", st, b)
	}
	cursor := b["nextCursor"].(string)
	firstID := b["items"].([]any)[0].(map[string]any)["id"]
	call(t, s, "POST", "/api/account-groups/move", token, `{"accountIds":["account-1"],"groupId":null}`)
	st, b, _ = call(t, s, "GET", "/api/accounts/account-1/logs?cursor="+cursor+"&action=group_move&result=success", token, "")
	if st != 200 || len(b["items"].([]any)) != 3 || b["nextCursor"] != nil {
		t.Fatalf("stable next page: %d %v", st, b)
	}
	for _, v := range b["items"].([]any) {
		if v.(map[string]any)["id"] == firstID {
			t.Fatal("duplicate across page")
		}
	}
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/check", token, `{"connectIfNeeded":true}`)
	if st != 503 {
		t.Fatalf("safe upstream status: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/account-1", token, "")
	if st != 200 || b["lastCheckResult"] != "failed" || b["lastCheckError"] != "GATEWAY_ERROR" {
		t.Fatalf("failed check not persisted: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/account-1/logs?result=failed", token, "")
	encoded, _ := json.Marshal(b)
	if st != 200 || len(b["items"].([]any)) != 2 || strings.Contains(string(encoded), "0123456789abcdef") || strings.Contains(string(encoded), "SQLite-secret") {
		t.Fatalf("unsafe error log: %d %v", st, b)
	}
}

type uploadPart struct{ name, body string }

func uploadAccounts(t *testing.T, s *httptest.Server, token string, parts []uploadPart, group string) (int, map[string]any) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, p := range parts {
		f, e := mw.CreateFormFile("files", p.name)
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte(p.body))
	}
	if group != "" {
		mw.WriteField("groupId", group)
	}
	mw.Close()
	req, _ := http.NewRequest("POST", s.URL+"/api/accounts/import", &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	out := map[string]any{}
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}
func TestAccountImportStrictPairingAliasesDuplicatesAndReconstruction(t *testing.T) {
	var registered atomic.Int32
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/register" {
			t.Errorf("import implicitly connected: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if b["apiId"] != float64(12345) || b["apiHash"] != "0123456789abcdef0123456789abcdef" {
			t.Errorf("normalized metadata not sent")
		}
		registered.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"accountId": b["accountId"], "status": "idle"})
	}))
	defer g.Close()
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.URL})
	admin, _ := login(t, s, "admin")
	viewer, _ := login(t, s, "viewer")
	metadata := `{"app_id":"12345","app_hash":"0123456789abcdef0123456789abcdef","phone":"+8613900000000","first_name":"Imported"}`
	parts := []uploadPart{{"prefix001.session", "SQLite format 3\x00test-one"}, {"prefix001.json", metadata}, {"1.session", "SQLite format 3\x00test-two"}, {"1.json", metadata}, {"dup.session", "SQLite format 3\x00a"}, {"dup.session", "SQLite format 3\x00b"}, {"dup.json", metadata}, {"missing.session", "SQLite format 3\x00c"}, {"bad.session", "not-sqlite"}, {"bad.json", metadata}, {"credential.session", "SQLite format 3\x00d"}, {"credential.json", `{"api_id":12345}`}}
	st, b := uploadAccounts(t, s, viewer, parts, "")
	if st != 403 {
		t.Fatalf("viewer import: %d %v", st, b)
	}
	st, b = uploadAccounts(t, s, admin, parts, "")
	if st != 200 {
		t.Fatalf("import: %d %v", st, b)
	}
	results := b["results"].([]any)
	if len(results) != 6 || registered.Load() != 2 {
		t.Fatalf("strict pair results: %v calls=%d", b, registered.Load())
	}
	var first string
	success := 0
	for _, r := range results {
		v := r.(map[string]any)
		if v["success"] == true {
			success++
			if v["basename"] == "prefix001" {
				first = v["accountId"].(string)
			}
		}
	}
	if success != 2 || first == "" {
		t.Fatal(b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/"+first, admin, "")
	if st != 200 || b["status"] != "idle" || b["phone"] != "+8613900000000" || b["phoneVerified"] != false {
		t.Fatalf("import profile not idle/unverified: %d %v", st, b)
	}
	a.Close()
	reopened, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	restored := httptest.NewServer(reopened.Handler())
	defer restored.Close()
	st, b = uploadAccounts(t, restored, admin, parts[:2], "")
	if st != 200 || registered.Load() != 2 || b["results"].([]any)[0].(map[string]any)["error"].(map[string]any)["code"] != "ACCOUNT_ALREADY_IMPORTED" {
		t.Fatalf("completed import overwritten: %d %v", st, b)
	}
	st, b = uploadAccounts(t, restored, admin, []uploadPart{{"new.session", "SQLite format 3\x00new"}, {"new.json", metadata}}, "missing")
	if st != 200 || registered.Load() != 2 || b["results"].([]any)[0].(map[string]any)["success"] != false {
		t.Fatalf("invalid group registered: %d %v", st, b)
	}
	st, b, _ = call(t, restored, "GET", "/api/accounts/"+first+"/logs", viewer, "")
	encoded, _ := json.Marshal(b)
	if st != 200 || strings.Contains(string(encoded), "0123456789abcdef") || strings.Contains(string(encoded), "SQLite") {
		t.Fatalf("unsafe import logs: %d %v", st, b)
	}
}

func TestAccountChecksReuseConnectionAndGroupsSnapshotSurvivesFailureAndRestart(t *testing.T) {
	var connects, checks, syncs atomic.Int32
	var failSync atomic.Bool
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/accounts/account-1/connect":
			connects.Add(1)
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			json.NewEncoder(w).Encode(map[string]any{"platformUserId": "1001", "connectionId": b["connectionId"]})
		case "/accounts/account-1/check":
			checks.Add(1)
			fmt.Fprint(w, `{"platformUserId":"1001","phone":"+8613800000000","firstName":"Ada","lastName":"Lovelace","username":"ada","premium":true,"source":"telegram"}`)
		case "/accounts/account-1/joined-groups":
			syncs.Add(1)
			if failSync.Load() {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"code":"NETWORK_TIMEOUT"}`)
				return
			}
			fmt.Fprint(w, `{"items":[{"id":"tg-1","title":"Existing group","username":null,"type":"supergroup","role":"creator"},{"id":"tg-2","title":"Peer group","username":"peer","type":"group","role":"unknown"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer g.Close()
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.URL})
	admin, _ := login(t, s, "admin")
	viewer, _ := login(t, s, "viewer")
	st, b, _ := call(t, s, "GET", "/api/accounts/account-1", viewer, "")
	if st != 200 || b["lastCheckedAt"] != nil || connects.Load() != 0 {
		t.Fatalf("read connected: %d %v", st, b)
	}
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/check", admin, `{"connectIfNeeded":false}`)
	if st != 409 || connects.Load() != 0 {
		t.Fatalf("implicit connect: %d %v", st, b)
	}
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/check", admin, `{"connectIfNeeded":true}`)
	if st != 200 || b["phone"] != "+8613800000000" || b["phoneVerified"] != true || b["lastCheckResult"] != "success" {
		t.Fatalf("explicit check: %d %v", st, b)
	}
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/check", admin, `{"connectIfNeeded":true}`)
	if st != 200 || connects.Load() != 1 || checks.Load() != 2 {
		t.Fatalf("online reused: %d %v connects=%d checks=%d", st, b, connects.Load(), checks.Load())
	}
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/joined-groups/sync", admin, `{}`)
	if st != 200 || len(b["items"].([]any)) != 2 || b["lastSyncedAt"] == nil {
		t.Fatalf("snapshot: %d %v", st, b)
	}
	synced := b["lastSyncedAt"]
	failSync.Store(true)
	st, b, _ = call(t, s, "POST", "/api/accounts/account-1/joined-groups/sync", admin, `{}`)
	if st != 503 {
		t.Fatalf("sync failure: %d %v", st, b)
	}
	a.Close()
	reopened, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	restored := httptest.NewServer(reopened.Handler())
	defer restored.Close()
	st, b, _ = call(t, restored, "GET", "/api/accounts/account-1/joined-groups", viewer, "")
	if st != 200 || len(b["items"].([]any)) != 2 || b["lastSyncedAt"] != synced || syncs.Load() != 2 {
		t.Fatalf("snapshot lost/re-read gateway: %d %v", st, b)
	}
	var managed []any
	readJSON(t, restored.URL, "/api/groups", viewer, &managed)
	if len(managed) != 0 {
		t.Fatal("discovery auto-managed group")
	}
	st, b, _ = call(t, restored, "POST", "/api/accounts/account-1/transition", admin, `{"expectedFrom":"online","to":"suspended"}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	st, b, _ = call(t, restored, "POST", "/api/accounts/account-1/check", admin, `{"connectIfNeeded":true}`)
	if st != 409 || connects.Load() != 1 {
		t.Fatalf("terminal revived: %d %v", st, b)
	}
	for _, p := range []string{"/api/accounts/account-1/check", "/api/accounts/account-1/joined-groups/sync"} {
		st, b, _ = call(t, restored, "POST", p, viewer, `{}`)
		if st != 403 {
			t.Fatalf("viewer write: %d %v", st, b)
		}
	}
	st, b, _ = call(t, restored, "GET", "/api/accounts/account-1/logs?action=sync_groups&result=failed", viewer, "")
	if st != 200 || len(b["items"].([]any)) != 1 {
		t.Fatalf("safe durable failure log: %d %v", st, b)
	}
}

func TestSimulatedAccountCheckNeverVerifiesTelegramPhone(t *testing.T) {
	g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/register" {
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			json.NewEncoder(w).Encode(map[string]any{"accountId": b["accountId"], "status": "idle"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/connect") {
			fmt.Fprint(w, `{"platformUserId":"simulated-1"}`)
			return
		}
		fmt.Fprint(w, `{"platformUserId":"simulated-1","phone":"+10000000001","firstName":"Fake","lastName":"Generated","username":"fake_user","source":"simulated","premium":false}`)
	}))
	defer g.Close()
	_, s := fixture(t, platform.Config{GatewayURL: g.URL})
	token, _ := login(t, s, "admin")
	st, imported := uploadAccounts(t, s, token, []uploadPart{{"original.session", "SQLite format 3\x00original"}, {"original.json", `{"api_id":12345,"api_hash":"0123456789abcdef0123456789abcdef","phone":"+8613900000000","first_name":"Imported","last_name":"Identity","username":"original_user"}`}}, "")
	if st != 200 {
		t.Fatal(st, imported)
	}
	id := imported["results"].([]any)[0].(map[string]any)["accountId"].(string)
	st, b, _ := call(t, s, "POST", "/api/accounts/"+id+"/check", token, `{"connectIfNeeded":true}`)
	if st != 200 || b["phoneVerified"] != false || b["verificationSource"] != "simulated" || b["phone"] != "+8613900000000" || b["firstName"] != "Imported" || b["lastName"] != "Identity" || b["username"] != "original_user" || b["premium"] != nil {
		t.Fatalf("fake phone presented as verified: %d %v", st, b)
	}
}

func TestAccountGroupsAtomicMovesDeletionAndViewerPermissions(t *testing.T) {
	_, s := fixture(t)
	admin, _ := login(t, s, "admin")
	viewer, _ := login(t, s, "viewer")
	st, group, _ := call(t, s, "POST", "/api/account-groups", admin, `{"name":"Operations"}`)
	if st != 201 {
		t.Fatalf("group create: %d %v", st, group)
	}
	id := group["id"].(string)
	st, b, _ := call(t, s, "POST", "/api/account-groups/move", admin, `{"accountIds":["account-1","missing"],"groupId":"`+id+`"}`)
	if st != 404 {
		t.Fatalf("invalid selection: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/account-1/logs?action=group_move&result=failed", admin, "")
	if st != 200 || len(b["items"].([]any)) != 1 {
		t.Fatalf("failed selection missing account log: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/account-1", admin, "")
	if st != 200 || b["groupId"] != nil {
		t.Fatalf("partial move: %d %v", st, b)
	}
	st, b, _ = call(t, s, "POST", "/api/account-groups/move", admin, `{"accountIds":["account-1","account-2"],"groupId":"`+id+`"}`)
	if st != 200 || b["updated"] != float64(2) {
		t.Fatalf("bulk move: %d %v", st, b)
	}
	st, b, _ = call(t, s, "PATCH", "/api/account-groups/"+id, admin, `{"name":"Renamed"}`)
	if st != 200 || b["name"] != "Renamed" || b["accountCount"] != float64(2) {
		t.Fatalf("rename: %d %v", st, b)
	}
	st, b, _ = call(t, s, "POST", "/api/account-groups", admin, `{"name":"Renamed"}`)
	if st != 409 {
		t.Fatalf("duplicate name: %d %v", st, b)
	}
	var groups []map[string]any
	if readJSON(t, s.URL, "/api/account-groups", viewer, &groups) != 200 || len(groups) != 1 || groups[0]["name"] != "Renamed" || groups[0]["accountCount"] != float64(2) {
		t.Fatalf("group list: %v", groups)
	}
	for _, p := range []struct{ method, path, body string }{{"POST", "/api/account-groups", `{"name":"no"}`}, {"PATCH", "/api/account-groups/" + id, `{"name":"no"}`}, {"DELETE", "/api/account-groups/" + id, ""}, {"POST", "/api/account-groups/move", `{"accountIds":["account-1"],"groupId":null}`}} {
		st, b, _ = call(t, s, p.method, p.path, viewer, p.body)
		if st != 403 {
			t.Fatalf("viewer %s: %d %v", p.path, st, b)
		}
	}
	st, b, _ = call(t, s, "DELETE", "/api/account-groups/"+id, admin, "")
	if st != 204 {
		t.Fatalf("delete: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/account-1", admin, "")
	if st != 200 || b["groupId"] != nil {
		t.Fatalf("delete should ungroup: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/account-1/logs", viewer, "")
	if st != 200 || len(b["items"].([]any)) < 2 {
		t.Fatalf("persisted move/delete logs: %d %v", st, b)
	}
}

func TestProfileErrorsOnlyExplicitAuthFailuresBecomeTerminal(t *testing.T) {
	for _, tc := range []struct {
		code, want string
		status     int
	}{{"NETWORK_TIMEOUT", "online", 503}, {"ACCOUNT_SUSPENDED", "suspended", 409}, {"SESSION_EXPIRED", "session_expired", 409}} {
		t.Run(tc.code, func(t *testing.T) {
			g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/connect") {
					fmt.Fprint(w, `{"platformUserId":"p-account-1"}`)
					return
				}
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]any{"code": tc.code})
			}))
			defer g.Close()
			_, s := fixture(t, platform.Config{GatewayURL: g.URL})
			token, _ := login(t, s, "admin")
			st, b, _ := call(t, s, "POST", "/api/accounts/account-1/check", token, `{"connectIfNeeded":true}`)
			if st != tc.status {
				t.Fatal(st, b)
			}
			st, b, _ = call(t, s, "GET", "/api/accounts/account-1", token, "")
			if st != 200 || b["status"] != tc.want || b["lastCheckResult"] != "failed" || b["lastCheckError"] != tc.code {
				t.Fatalf("wrong account health: %d %v", st, b)
			}
		})
	}
}
