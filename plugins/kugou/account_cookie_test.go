package kugou

import "testing"

func TestParseKugouCredentials(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantToken string
		wantUser  string
	}{
		{
			name:      "plain cookie header",
			raw:       "token=abc123; userid=987654; other=x",
			wantToken: "abc123",
			wantUser:  "987654",
		},
		{
			name:      "KuGoo blob with & separated pairs",
			raw:       "KuGoo=KugooID=987654&t=abc123&vip=1; mid=zzz",
			wantToken: "abc123",
			wantUser:  "987654",
		},
		{
			name:      "case insensitive and spaced",
			raw:       " UserID = 42 ;  Token = tok ",
			wantToken: "tok",
			wantUser:  "42",
		},
		{
			name:      "multi-line (cookies.txt already normalised)",
			raw:       "token=t1\nuserid=u1",
			wantToken: "t1",
			wantUser:  "u1",
		},
		{
			name: "missing values",
			raw:  "foo=bar; baz=qux",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, user := parseKugouCredentials(tc.raw)
			if token != tc.wantToken || user != tc.wantUser {
				t.Errorf("parseKugouCredentials(%q) = (%q, %q), want (%q, %q)",
					tc.raw, token, user, tc.wantToken, tc.wantUser)
			}
		})
	}
}
