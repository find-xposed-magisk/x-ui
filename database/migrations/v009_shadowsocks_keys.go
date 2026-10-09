package migrations

import (
	"encoding/json"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/xray"

	"gorm.io/gorm"
)

// migrateV009ShadowsocksKeys replaces Shadowsocks 2022 keys of the wrong size.
// The panel used to generate 32-byte keys whatever the method, so every
// 2022-blake3-aes-128-gcm inbound got keys the core refuses, and the core then
// does not start at all. Since such an inbound has never worked, no client
// holds a working copy of these keys, and new ones lose nothing.
func migrateV009ShadowsocksKeys(db *gorm.DB) error {
	tx := db.Begin()
	var err error
	defer func() {
		if err == nil {
			tx.Commit()
		} else {
			tx.Rollback()
		}
	}()

	var inbounds []*model.Inbound
	err = tx.Model(model.Inbound{}).Where("protocol = ?", string(model.Shadowsocks)).Find(&inbounds).Error
	if err != nil {
		return err
	}

	for _, inbound := range inbounds {
		settings := map[string]any{}
		if json.Unmarshal([]byte(inbound.Settings), &settings) != nil {
			continue
		}
		method, _ := settings["method"].(string)
		length := xray.Shadowsocks2022KeyLength(method)
		if length == 0 {
			continue
		}

		changed := false
		renew := func(holder map[string]any) {
			if key, _ := holder["password"].(string); !xray.ValidShadowsocks2022Key(key, length) {
				holder["password"] = xray.NewShadowsocks2022Key(length)
				changed = true
			}
		}
		renew(settings)
		clients, _ := settings["clients"].([]any)
		for _, client := range clients {
			if c, ok := client.(map[string]any); ok {
				renew(c)
			}
		}
		if !changed {
			continue
		}

		var modified []byte
		modified, err = json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return err
		}
		err = tx.Model(model.Inbound{}).Where("id = ?", inbound.Id).Update("settings", string(modified)).Error
		if err != nil {
			return err
		}
		logger.Warningf("inbound %q: replaced %s keys of the wrong size; its clients need new links", inbound.Tag, method)
	}

	return nil
}
