package migrations

import (
	"encoding/json"
	"testing"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/xray"
)

func TestV009ReplacesShadowsocks2022KeysOfTheWrongSize(t *testing.T) {
	db := newTestDB(t)
	good16, bad32, good32 := xray.NewShadowsocks2022Key(16), xray.NewShadowsocks2022Key(32), xray.NewShadowsocks2022Key(32)
	seed := []*model.Inbound{
		{Tag: "aes128", Protocol: model.Shadowsocks, Settings: `{"method":"2022-blake3-aes-128-gcm","password":"` + bad32 +
			`","clients":[{"email":"a","password":"` + bad32 + `"},{"email":"b","password":"` + good16 + `"}]}`},
		{Tag: "aes256", Protocol: model.Shadowsocks, Settings: `{"method":"2022-blake3-aes-256-gcm","password":"` + good32 + `","clients":[]}`},
	}
	for _, in := range seed {
		if err := db.Create(in).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := migrateV009ShadowsocksKeys(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var after []model.Inbound
	db.Order("id").Find(&after)
	if err := xray.CheckShadowsocks2022Keys("shadowsocks", after[0].Settings); err != nil {
		t.Fatalf("aes-128 inbound still invalid: %v", err)
	}
	var s struct {
		Clients []struct{ Password string } `json:"clients"`
	}
	json.Unmarshal([]byte(after[0].Settings), &s)
	if s.Clients[1].Password != good16 {
		t.Errorf("a valid client key was replaced")
	}
	if after[1].Settings != seed[1].Settings {
		t.Errorf("a valid inbound was rewritten")
	}
}
