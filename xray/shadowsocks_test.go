package xray

import (
	"strings"
	"testing"
)

func TestCheckShadowsocks2022Keys(t *testing.T) {
	key16, key32 := NewShadowsocks2022Key(16), NewShadowsocks2022Key(32)
	cases := []struct {
		name, settings, wantErr string
	}{
		{"aes-128 with 16-byte keys",
			`{"method":"2022-blake3-aes-128-gcm","password":"` + key16 + `","clients":[{"email":"a","password":"` + key16 + `"}]}`, ""},
		{"aes-128 with the old 32-byte inbound key",
			`{"method":"2022-blake3-aes-128-gcm","password":"` + key32 + `","clients":[]}`, "inbound password"},
		{"aes-128 with a 32-byte client key",
			`{"method":"2022-blake3-aes-128-gcm","password":"` + key16 + `","clients":[{"email":"bob","password":"` + key32 + `"}]}`, "password of bob"},
		{"aes-256 with a 32-byte key", `{"method":"2022-blake3-aes-256-gcm","password":"` + key32 + `"}`, ""},
		{"a raw 32-character key, as the core accepts it",
			`{"method":"2022-blake3-aes-256-gcm","password":"` + strings.Repeat("x!", 16) + `"}`, ""},
		{"classic Shadowsocks takes any password", `{"method":"aes-256-gcm","password":"short"}`, ""},
	}
	for _, c := range cases {
		err := CheckShadowsocks2022Keys("shadowsocks", c.settings)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: error %v, want one mentioning %q", c.name, err, c.wantErr)
		}
	}
	if err := CheckShadowsocks2022Keys("vless", `{"method":"2022-blake3-aes-128-gcm","password":"x"}`); err != nil {
		t.Errorf("checked a non-Shadowsocks inbound: %v", err)
	}
}
