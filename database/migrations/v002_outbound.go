package migrations

import (
	"encoding/json"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"

	"gorm.io/gorm"
)

func migrateV002Outbound(db *gorm.DB) error {
	var count int64
	if err := db.Model(&model.Outbound{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	if err := migrateOutboundsFromTemplate(db); err != nil {
		return err
	}

	if err := db.Model(&model.Outbound{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		initDefaultOutbounds(db)
	}
	return nil
}

func migrateOutboundsFromTemplate(db *gorm.DB) error {
	templateConfig, err := getXrayTemplate(db)
	if err != nil {
		logger.Warning("outbound migration: get xray template failed:", err)
		return nil
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(templateConfig), &cfg); err != nil {
		logger.Warning("outbound migration: parse template failed:", err)
		return nil
	}

	rawOutbounds, ok := cfg["outbounds"].([]interface{})
	if !ok || len(rawOutbounds) == 0 {
		return nil
	}

	migrated := 0
	for i, item := range rawOutbounds {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		outbound := outboundFromMap(raw, i)
		if outbound.Tag == "" || outbound.Protocol == "" {
			continue
		}
		if err := db.Create(outbound).Error; err != nil {
			logger.Warning("outbound migration: create failed:", err)
			continue
		}
		migrated++
	}
	if migrated == 0 {
		return nil
	}

	cfg["outbounds"] = []interface{}{}
	newTemplate, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		logger.Warning("outbound migration: marshal template failed:", err)
		return nil
	}
	if err := saveXrayTemplate(db, string(newTemplate)); err != nil {
		logger.Warning("outbound migration: save template failed:", err)
		return nil
	}
	logger.Info("Migrated", migrated, "outbound(s) from xray settings to database")
	return nil
}

func initDefaultOutbounds(db *gorm.DB) {
	for _, outbound := range defaultOutbounds() {
		if err := db.Create(outbound).Error; err != nil {
			logger.Warning("outbound init: create default failed:", err)
		}
	}
	logger.Info("Initialized default outbound(s): direct, blocked")
}

func outboundFromMap(raw map[string]interface{}, sort int) *model.Outbound {
	o := &model.Outbound{Sort: sort}
	if v, ok := raw["sendThrough"].(string); ok {
		o.SendThrough = v
	}
	if v, ok := raw["protocol"].(string); ok {
		o.Protocol = v
	}
	if v, ok := raw["tag"].(string); ok {
		o.Tag = v
	}
	if v, ok := raw["targetStrategy"].(string); ok {
		o.TargetStrategy = v
	}
	if v, ok := raw["settings"]; ok && v != nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		o.Settings = string(b)
	}
	if v, ok := raw["streamSettings"]; ok && v != nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		o.StreamSettings = string(b)
	}
	if v, ok := raw["proxySettings"]; ok && v != nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		o.ProxySettings = string(b)
	}
	if v, ok := raw["mux"]; ok && v != nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		o.Mux = string(b)
	}
	return o
}

func defaultOutbounds() []*model.Outbound {
	return []*model.Outbound{
		{
			Sort:     0,
			Protocol: "freedom",
			Tag:      "direct",
			Settings: `{"domainStrategy":"UseIP","noises":[],"redirect":""}`,
		},
		{
			Sort:     1,
			Protocol: "blackhole",
			Tag:      "blocked",
			Settings: `{}`,
		},
	}
}
