package migrations

import (
	"encoding/json"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/xray"

	"gorm.io/gorm"
)

// migrateV008OutboundCore rewrites stored outbounds for what Xray-core v26.9.30
// removed. The core rejects "proxySettings" outright, and silently ignores
// "finalmask.quicParams.udpHop" and WireGuard's "domainStrategy", so without
// this step port hopping and the chosen resolution would just stop working.
func migrateV008OutboundCore(db *gorm.DB) error {
	tx := db.Begin()
	var err error
	defer func() {
		if err == nil {
			tx.Commit()
		} else {
			tx.Rollback()
		}
	}()

	var outbounds []*model.Outbound
	err = tx.Model(model.Outbound{}).Find(&outbounds).Error
	if err != nil {
		return err
	}

	for _, outbound := range outbounds {
		changed := false

		stream := map[string]any{}
		if outbound.StreamSettings != "" && json.Unmarshal([]byte(outbound.StreamSettings), &stream) != nil {
			continue
		}
		if xray.MoveUDPHopToMask(stream) {
			changed = true
		}

		if outbound.ProxySettings != "" {
			var proxy struct {
				Tag string `json:"tag"`
			}
			_ = json.Unmarshal([]byte(outbound.ProxySettings), &proxy)
			if proxy.Tag != "" {
				sockopt, _ := stream["sockopt"].(map[string]any)
				if sockopt == nil {
					sockopt = map[string]any{}
					stream["sockopt"] = sockopt
				}
				if dialer, _ := sockopt["dialerProxy"].(string); dialer == "" {
					sockopt["dialerProxy"] = proxy.Tag
				}
			}
			outbound.ProxySettings = ""
			changed = true
		}

		if outbound.Protocol == "wireguard" {
			settings := map[string]any{}
			if json.Unmarshal([]byte(outbound.Settings), &settings) == nil {
				if strategy, ok := settings["domainStrategy"]; ok {
					if s, _ := strategy.(string); s != "" && outbound.TargetStrategy == "" {
						outbound.TargetStrategy = s
					}
					delete(settings, "domainStrategy")
					var modified []byte
					modified, err = json.MarshalIndent(settings, "", "  ")
					if err != nil {
						return err
					}
					outbound.Settings = string(modified)
					changed = true
				}
			}
		}

		if !changed {
			continue
		}
		if len(stream) > 0 {
			var modified []byte
			modified, err = json.MarshalIndent(stream, "", "  ")
			if err != nil {
				return err
			}
			outbound.StreamSettings = string(modified)
		} else {
			outbound.StreamSettings = ""
		}

		err = tx.Model(model.Outbound{}).Where("id = ?", outbound.Id).Updates(map[string]any{
			"settings":        outbound.Settings,
			"stream_settings": outbound.StreamSettings,
			"proxy_settings":  outbound.ProxySettings,
			"target_strategy": outbound.TargetStrategy,
		}).Error
		if err != nil {
			return err
		}
	}

	return nil
}
