package gateway

import "testing"

func TestGroupLinksOnlyAcceptTelegramGroups(t *testing.T) {
	for _, tc := range []struct{ raw, name, hash string }{{"https://t.me/example_group", "example_group", ""}, {"@example_group", "example_group", ""}, {"t.me/+abc_123-", "", "abc_123-"}, {"https://t.me/joinchat/abc123/", "", "abc123"}} {
		name, hash, err := parseGroupLink(tc.raw)
		if err != nil || name != tc.name || hash != tc.hash {
			t.Fatalf("parse %q: %s %s %v", tc.raw, name, hash, err)
		}
	}
	for _, raw := range []string{"", "https://evil.test/test", "https://t.me.evil.test/test", "https://t.me:443/test", "https://evil@t.me/test", "https://t.me/test/123", "https://t.me/+", "https://t.me/test?x=1", "file:///tmp/session"} {
		if _, _, err := parseGroupLink(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
