package entity

import (
	"strings"
	"testing"
)

// validSetting is the smallest settings object that passes CheckValid, so each
// test can change one field and see only that field's effect.
func validSetting() *AllSetting {
	return &AllSetting{
		WebPort:      2053,
		SubPort:      2096,
		WebBasePath:  "/",
		SubPath:      "/sub/",
		SubJsonPath:  "/json/",
		TimeLocation: "Asia/Tehran",
	}
}

func TestCheckValidAcceptsTheBaseline(t *testing.T) {
	if err := validSetting().CheckValid(); err != nil {
		t.Fatalf("the baseline settings were rejected: %v", err)
	}
}

func TestCheckValidTgBotProxy(t *testing.T) {
	tests := []struct {
		name     string
		proxy    string
		accepted bool
	}{
		{"empty means direct", "", true},
		{"socks5", "socks5://127.0.0.1:1080", true},
		{"socks5 with auth", "socks5://user:pass@127.0.0.1:1080", true},
		{"http", "http://127.0.0.1:8080", true},
		{"missing scheme", "127.0.0.1:1080", false},
		{"unsupported scheme", "ftp://127.0.0.1:21", false},
		{"missing port", "socks5://127.0.0.1", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting := validSetting()
			setting.TgBotProxy = test.proxy
			err := setting.CheckValid()
			if test.accepted && err != nil {
				t.Fatalf("proxy %q was rejected: %v", test.proxy, err)
			}
			if !test.accepted && err == nil {
				t.Fatalf("proxy %q should have been rejected", test.proxy)
			}
		})
	}
}

func TestCheckValidTgBotChatId(t *testing.T) {
	tests := []struct {
		name     string
		chatId   string
		accepted bool
	}{
		{"empty", "", true},
		{"single id", "123456789", true},
		{"several ids", "123456789,-1001234567890", true},
		{"supergroup topic", "-1001234567890:42", true},
		{"not a number", "@username", false},
		{"bad topic", "-1001234567890:abc", false},
		{"zero topic", "-1001234567890:0", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting := validSetting()
			setting.TgBotChatId = test.chatId
			err := setting.CheckValid()
			if test.accepted && err != nil {
				t.Fatalf("chat id %q was rejected: %v", test.chatId, err)
			}
			if !test.accepted && err == nil {
				t.Fatalf("chat id %q should have been rejected", test.chatId)
			}
		})
	}
}

// CheckValid also normalises the paths it is given; a regression here silently
// breaks every subscription URL.
func TestCheckValidNormalisesPaths(t *testing.T) {
	setting := validSetting()
	setting.WebBasePath = "panel"
	setting.SubPath = "sub"
	setting.SubJsonPath = "json"

	if err := setting.CheckValid(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for name, got := range map[string]string{
		"WebBasePath": setting.WebBasePath,
		"SubPath":     setting.SubPath,
		"SubJsonPath": setting.SubJsonPath,
	} {
		if !strings.HasPrefix(got, "/") || !strings.HasSuffix(got, "/") {
			t.Errorf("%s = %q, want it wrapped in slashes", name, got)
		}
	}
}

func TestCheckValidRejectsPortClash(t *testing.T) {
	setting := validSetting()
	setting.SubPort = setting.WebPort
	if err := setting.CheckValid(); err == nil {
		t.Fatal("the panel and subscription ports must not be allowed to match")
	}
}
