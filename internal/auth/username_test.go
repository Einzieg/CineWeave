package auth

import "testing"

func TestNormalizeUsername(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		want       string
		normalized string
		wantErr    bool
	}{
		{name: "mixed case", input: "  Cine_User-01  ", want: "Cine_User-01", normalized: "cine_user-01"},
		{name: "minimum", input: "abc", want: "abc", normalized: "abc"},
		{name: "too short", input: "ab", wantErr: true},
		{name: "email", input: "user@example.com", wantErr: true},
		{name: "leading separator", input: "_user", wantErr: true},
		{name: "trailing separator", input: "user-", wantErr: true},
		{name: "unicode", input: "用户123", wantErr: true},
		{name: "reserved", input: "ADMIN", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			username, normalized, err := NormalizeUsername(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("NormalizeUsername(%q) expected error", test.input)
				}
				return
			}
			if err != nil || username != test.want || normalized != test.normalized {
				t.Fatalf("NormalizeUsername(%q) = %q, %q, %v", test.input, username, normalized, err)
			}
		})
	}
}

func TestNormalizeLoginIdentifier(t *testing.T) {
	if got, email := NormalizeLoginIdentifier(" Admin_User "); got != "admin_user" || email {
		t.Fatalf("username identifier = %q, %v", got, email)
	}
	if got, email := NormalizeLoginIdentifier(" User@Example.COM "); got != "user@example.com" || !email {
		t.Fatalf("email identifier = %q, %v", got, email)
	}
}
