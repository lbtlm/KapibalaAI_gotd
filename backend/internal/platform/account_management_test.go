package platform_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
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
func TestAccountGroupsAtomicMovesDeletionAndViewerPermissions(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	insertAccountRows(t, cfg, "account-1", "account-2")
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
