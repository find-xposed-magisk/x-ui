package sub

import (
	"regexp"
	"strings"
	"testing"

	"github.com/alireza0/x-ui/database/model"
)

func testService() *SubService {
	service := NewSubService(false, "-ieo")
	service.address = "example.com"
	return service
}

func inboundFor(protocol model.Protocol, settings, stream string) *model.Inbound {
	return &model.Inbound{
		Id: 1, Port: 443, Protocol: protocol, Tag: "inbound",
		Remark: "R", Settings: settings, StreamSettings: stream,
	}
}

// Xray accepts far looser transport settings than the link builders used to
// assume: a WebSocket inbound needs no explicit path, a gRPC one no multiMode,
// and streamSettings may be empty entirely. Every case below panicked before
// these fields were read defensively, and because a subscription is built from
// every inbound a client belongs to, one such inbound failed the whole
// subscription rather than just its own link.
func TestLinkBuildersSurviveIncompleteConfigs(t *testing.T) {
	tests := []struct {
		name     string
		protocol model.Protocol
		settings string
		stream   string
	}{
		{"vmess ws without path", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"ws","wsSettings":{}}`},
		{"vmess ws without settings", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"ws"}`},
		{"vmess tcp http without request", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"tcp","tcpSettings":{"header":{"type":"http"}}}`},
		{"vmess tcp http with empty path list", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"tcp","tcpSettings":{"header":{"type":"http","request":{"path":[]}}}}`},
		{"vmess grpc without multiMode", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"grpc","grpcSettings":{}}`},
		{"vmess httpupgrade without path", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"httpupgrade","httpupgradeSettings":{}}`},
		{"vmess xhttp without path", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"xhttp","xhttpSettings":{}}`},
		{"vmess kcp without seed", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"kcp","kcpSettings":{}}`},
		{"vmess tls without settings", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"ws","wsSettings":{"path":"/x"},"security":"tls"}`},
		{"vmess reality without settings", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"tcp","security":"reality"}`},
		{"vmess empty stream", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{}`},
		{"vmess unparsable stream", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `not json`},

		{"vless ws without path", "vless", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"ws","wsSettings":{}}`},
		{"vless tcp http without request", "vless", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"tcp","tcpSettings":{"header":{"type":"http"}}}`},
		{"vless xhttp without path", "vless", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"xhttp","xhttpSettings":{}}`},
		{"vless grpc without serviceName", "vless", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"grpc","grpcSettings":{}}`},
		{"vless reality with empty server names", "vless", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"tcp","security":"reality","realitySettings":{"serverNames":[],"shortIds":[]}}`},
		{"vless empty stream", "vless", `{"clients":[{"id":"u","email":"e"}]}`, `{}`},

		{"trojan ws without path", "trojan", `{"clients":[{"password":"p","email":"e"}]}`, `{"network":"ws","wsSettings":{}}`},
		{"trojan tcp http without request", "trojan", `{"clients":[{"password":"p","email":"e"}]}`, `{"network":"tcp","tcpSettings":{"header":{"type":"http"}}}`},
		{"trojan xhttp without path", "trojan", `{"clients":[{"password":"p","email":"e"}]}`, `{"network":"xhttp","xhttpSettings":{}}`},
		{"trojan empty stream", "trojan", `{"clients":[{"password":"p","email":"e"}]}`, `{}`},

		{"shadowsocks minimal", "shadowsocks", `{"method":"aes-256-gcm","password":"p","clients":[{"password":"c","email":"e"}]}`, `{"network":"tcp"}`},
		{"shadowsocks ws without path", "shadowsocks", `{"method":"aes-256-gcm","password":"p","clients":[{"password":"c","email":"e"}]}`, `{"network":"ws","wsSettings":{}}`},
		{"shadowsocks without method", "shadowsocks", `{"clients":[{"password":"c","email":"e"}]}`, `{"network":"tcp"}`},

		{"hysteria minimal", "hysteria", `{"clients":[{"auth":"a","email":"e"}]}`, `{"security":"tls"}`},
		{"hysteria without security", "hysteria", `{"clients":[{"auth":"a","email":"e"}]}`, `{}`},

		{"external proxy without fields", "vmess", `{"clients":[{"id":"u","email":"e"}]}`, `{"network":"ws","wsSettings":{"path":"/x"},"externalProxy":[{}]}`},
	}

	service := testService()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			service.getLink(inboundFor(test.protocol, test.settings, test.stream), "e")
		})
	}
}

// A caller asking for an email the inbound does not serve must get nothing back
// rather than an index panic.
func TestLinkBuildersRejectUnknownEmail(t *testing.T) {
	service := testService()
	for _, protocol := range []model.Protocol{"vmess", "vless", "trojan", "shadowsocks", "hysteria"} {
		t.Run(string(protocol), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			settings := `{"method":"aes-256-gcm","password":"p","clients":[{"id":"u","password":"p","auth":"a","email":"someone"}]}`
			if link := service.getLink(inboundFor(protocol, settings, `{"network":"tcp"}`), "nobody"); link != "" {
				t.Fatalf("link = %q, want empty for an email this inbound does not serve", link)
			}
		})
	}
}

// Well-formed configurations must still produce exactly the links they did
// before the builders were made defensive.
func TestLinksForValidConfigs(t *testing.T) {
	tests := []struct {
		name     string
		protocol model.Protocol
		settings string
		stream   string
		want     string
	}{
		{
			name: "vless reality", protocol: "vless",
			settings: `{"clients":[{"id":"11111111-2222-3333-4444-555555555555","email":"e","flow":"xtls-rprx-vision"}]}`,
			stream:   `{"network":"tcp","security":"reality","realitySettings":{"serverNames":["www.example.com"],"shortIds":["abcd1234"],"settings":{"publicKey":"PBK","fingerprint":"chrome","spiderX":"/"}}}`,
			// spx carries a random suffix, so it is normalised before comparing.
			want: "vless://11111111-2222-3333-4444-555555555555@example.com:443?flow=xtls-rprx-vision&fp=chrome&pbk=PBK&security=reality&sid=abcd1234&sni=www.example.com&spx=RANDOM&type=tcp#R-e",
		},
		{
			name: "vless xhttp", protocol: "vless",
			settings: `{"clients":[{"id":"11111111-2222-3333-4444-555555555555","email":"e"}]}`,
			stream:   `{"network":"xhttp","xhttpSettings":{"path":"/x","host":"x.example.com","mode":"packet-up"}}`,
			want:     "vless://11111111-2222-3333-4444-555555555555@example.com:443?host=x.example.com&mode=packet-up&path=%2Fx&security=none&type=xhttp#R-e",
		},
		{
			name: "trojan ws over tls", protocol: "trojan",
			settings: `{"clients":[{"password":"tp","email":"e"}]}`,
			stream:   `{"network":"ws","security":"tls","wsSettings":{"path":"/t","host":"t.example.com"},"tlsSettings":{"serverName":"t.example.com","alpn":["h2"],"settings":{"fingerprint":"firefox"}}}`,
			want:     "trojan://tp@example.com:443?alpn=h2&fp=firefox&host=t.example.com&path=%2Ft&security=tls&sni=t.example.com&type=ws#R-e",
		},
	}

	spiderX := regexp.MustCompile(`spx=[^&#]*`)
	service := testService()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := service.getLink(inboundFor(test.protocol, test.settings, test.stream), "e")
			got = spiderX.ReplaceAllString(got, "spx=RANDOM")
			if got != test.want {
				t.Errorf("link mismatch\n got: %s\nwant: %s", got, test.want)
			}
		})
	}
}

// The 2022 Shadowsocks ciphers put the server password in the userinfo; the
// older ones do not.
func TestShadowsocksEncodesTheRightPassword(t *testing.T) {
	service := testService()

	legacy := service.getLink(inboundFor("shadowsocks",
		`{"method":"aes-256-gcm","password":"srvpass","clients":[{"password":"cli","email":"e"}]}`,
		`{"network":"tcp"}`), "e")
	blake3 := service.getLink(inboundFor("shadowsocks",
		`{"method":"2022-blake3-aes-256-gcm","password":"srvpass","clients":[{"password":"cli","email":"e"}]}`,
		`{"network":"tcp"}`), "e")

	if legacy == "" || blake3 == "" {
		t.Fatal("a shadowsocks link was not produced")
	}
	if legacy == blake3 {
		t.Fatal("the 2022 and legacy ciphers produced the same userinfo")
	}
	if !strings.HasPrefix(legacy, "ss://") || !strings.HasPrefix(blake3, "ss://") {
		t.Fatalf("unexpected scheme:\n%s\n%s", legacy, blake3)
	}
}
