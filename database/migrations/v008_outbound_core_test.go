package migrations

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/alireza0/x-ui/database/model"
)

func runV008(t *testing.T, outbounds ...*model.Outbound) []model.Outbound {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&model.Outbound{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	for _, o := range outbounds {
		if err := db.Create(o).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := migrateV008OutboundCore(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var after []model.Outbound
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	return after
}

func assertSameJSON(t *testing.T, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("got is not JSON: %q", got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %q", want)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestV008MovesUDPHopToMask(t *testing.T) {
	after := runV008(t, &model.Outbound{
		Tag:            "hy",
		Protocol:       "hysteria",
		Settings:       `{"version":2}`,
		StreamSettings: `{"network":"hysteria","finalmask":{"quicParams":{"udpHop":{"ports":"20000-50000","interval":"10"}}}}`,
	})
	assertSameJSON(t, after[0].StreamSettings, `{"network":"hysteria","finalmask":{"udp":[
		{"type":"udphop","settings":{"mode":"intervalLocal,intervalRemote","remotePorts":"20000-50000","interval":"10"}}]}}`)
}

func TestV008ProxySettingsBecomeDialerProxy(t *testing.T) {
	after := runV008(t,
		&model.Outbound{
			Tag:           "chained",
			Protocol:      "vless",
			Settings:      `{}`,
			ProxySettings: `{"tag":"front","transportLayer":true}`,
		},
		&model.Outbound{
			Tag:            "kept",
			Protocol:       "vless",
			Settings:       `{}`,
			StreamSettings: `{"sockopt":{"dialerProxy":"other"}}`,
			ProxySettings:  `{"tag":"front"}`,
		},
	)
	if after[0].ProxySettings != "" || after[1].ProxySettings != "" {
		t.Fatalf("proxySettings left behind: %q, %q", after[0].ProxySettings, after[1].ProxySettings)
	}
	assertSameJSON(t, after[0].StreamSettings, `{"sockopt":{"dialerProxy":"front"}}`)
	assertSameJSON(t, after[1].StreamSettings, `{"sockopt":{"dialerProxy":"other"}}`)
}

func TestV008WireguardDomainStrategyBecomesTargetStrategy(t *testing.T) {
	after := runV008(t,
		&model.Outbound{
			Tag:      "warp",
			Protocol: "wireguard",
			Settings: `{"secretKey":"k","domainStrategy":"ForceIPv4"}`,
		},
		&model.Outbound{
			Tag:            "wg",
			Protocol:       "wireguard",
			Settings:       `{"secretKey":"k","domainStrategy":"ForceIPv6"}`,
			TargetStrategy: "UseIPv4",
		},
	)
	assertSameJSON(t, after[0].Settings, `{"secretKey":"k"}`)
	if after[0].TargetStrategy != "ForceIPv4" {
		t.Fatalf("targetStrategy = %q", after[0].TargetStrategy)
	}
	if after[1].TargetStrategy != "UseIPv4" {
		t.Fatalf("an explicit targetStrategy was overwritten: %q", after[1].TargetStrategy)
	}
}

func TestV008LeavesOtherOutboundsAlone(t *testing.T) {
	const stream = "{\n  \"network\": \"tcp\"\n}"
	after := runV008(t, &model.Outbound{
		Tag:            "direct",
		Protocol:       "freedom",
		Settings:       `{"domainStrategy":"UseIP"}`,
		StreamSettings: stream,
	})
	if after[0].Settings != `{"domainStrategy":"UseIP"}` || after[0].StreamSettings != stream {
		t.Fatalf("untouched outbound was rewritten: %+v", after[0])
	}
}
