package platform

import (
	"testing"
)

func TestImportMetadataProfile(t *testing.T) {
	for _, tc := range []struct{ name, fields, phone, first, last, username string }{
		{"standard", `"phone":"+1 (202) 555-0123","first_name":" Ada ","last_name":"Lovelace","username":"@ada"`, "12025550123", "Ada", "Lovelace", "ada"},
		{"aliases", `"phone_number":"12025550124","firstName":"Grace","lastName":"Hopper","user_name":"grace"`, "12025550124", "Grace", "Hopper", "grace"},
		{"filename fallback", `"phone":"","first_name":"File name"`, "12025550999", "File name", "", ""},
		{"numeric phone", `"phone":12025550125`, "12025550125", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, p, err := parseImportMetadata([]byte(`{"api_id":12345,"api_hash":"0123456789abcdef0123456789abcdef",`+tc.fields+`}`), "12025550999")
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range []struct {
				got  *string
				want string
			}{{p.phone, tc.phone}, {p.firstName, tc.first}, {p.lastName, tc.last}, {p.username, tc.username}} {
				got := ""
				if field.got != nil {
					got = *field.got
				}
				if got != field.want {
					t.Errorf("profile field = %q, want %q", got, field.want)
				}
			}
		})
	}
}

func TestImportMetadataFilenameDoesNotInventIdentity(t *testing.T) {
	for _, tc := range []struct{ filename, want string }{
		{"+12025550123", "12025550123"}, {"12025550123_backup", "12025550123"},
		{"account_12025550123", "12025550123"}, {"account-1", ""},
		{"123456789012345678901", ""}, {"label12345678suffix", ""},
	} {
		_, p, err := parseImportMetadata([]byte(`{"api_id":12345,"api_hash":"0123456789abcdef0123456789abcdef"}`), tc.filename)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if p.phone != nil {
			got = *p.phone
		}
		if got != tc.want {
			t.Errorf("filename %q: got %q, want %q", tc.filename, got, tc.want)
		}
	}
}
