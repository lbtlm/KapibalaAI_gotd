package gateway_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestInviteAcceptsOmittedBody(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	gid := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	req, _ := http.NewRequest("POST", ts.URL+"/groups/"+gid+"/invite", nil)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != 200 || body["inviteLink"] == nil {
		t.Fatalf("omitted-body invite: %d %v", res.StatusCode, body)
	}
}
