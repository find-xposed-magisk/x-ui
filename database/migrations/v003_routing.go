package migrations

import (
	"encoding/json"
	"fmt"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"

	"gorm.io/gorm"
)

func migrateV003Routing(db *gorm.DB) error {
	templateConfig, err := getXrayTemplate(db)
	if err != nil {
		logger.Warning("routing rule migration: get xray template failed:", err)
		return nil
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(templateConfig), &cfg); err != nil {
		logger.Warning("routing rule migration: parse template failed:", err)
		return nil
	}

	changed := migrateLegacyApiConfig(cfg)
	if changed {
		newTemplate, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			logger.Warning("routing rule migration: marshal legacy api config failed:", err)
			return nil
		}
		if err := saveXrayTemplate(db, string(newTemplate)); err != nil {
			logger.Warning("routing rule migration: save legacy api config failed:", err)
			return nil
		}
		logger.Info("Migrated legacy api inbound to api.listen in xray settings")
	}

	var count int64
	if err := db.Model(&model.RoutingRule{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	routing, ok := cfg["routing"].(map[string]interface{})
	if !ok {
		return nil
	}
	rawRules, ok := routing["rules"].([]interface{})
	if !ok || len(rawRules) == 0 {
		return nil
	}

	for i, item := range rawRules {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		rule := routingRuleFromMap(raw, i)
		if err := db.Create(rule).Error; err != nil {
			logger.Warning("routing rule migration: create failed:", err)
		}
	}

	routing["rules"] = []interface{}{}
	cfg["routing"] = routing
	newTemplate, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		logger.Warning("routing rule migration: marshal template failed:", err)
		return nil
	}
	if err := saveXrayTemplate(db, string(newTemplate)); err != nil {
		logger.Warning("routing rule migration: save template failed:", err)
		return nil
	}
	logger.Info("Migrated", len(rawRules), "routing rule(s) from xray settings to database")
	return nil
}

func apiListenMissing(api map[string]interface{}) bool {
	_, ok := api["listen"]
	return !ok
}

func defaultApiListen() string {
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(config.GetDefaultXrayTemplate()), &cfg); err != nil {
		return "127.0.0.1:62789"
	}
	api, ok := cfg["api"].(map[string]interface{})
	if !ok {
		return "127.0.0.1:62789"
	}
	s, _ := api["listen"].(string)
	if s == "" {
		return "127.0.0.1:62789"
	}
	return s
}

func inboundListenAddress(inbound map[string]interface{}) string {
	port := 0
	switch p := inbound["port"].(type) {
	case float64:
		port = int(p)
	case int:
		port = p
	case int64:
		port = int(p)
	}
	if port == 0 {
		return ""
	}

	host := "127.0.0.1"
	if listen, ok := inbound["listen"]; ok {
		switch v := listen.(type) {
		case string:
			if v != "" {
				host = v
			}
		case []interface{}:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok && s != "" {
					host = s
				}
			}
		}
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func isLegacyApiRoutingRule(rule map[string]interface{}) bool {
	outboundTag, _ := rule["outboundTag"].(string)
	if outboundTag != "api" {
		return false
	}
	val, ok := rule["inboundTag"]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case []interface{}:
		return len(v) == 1 && v[0] == "api"
	case []string:
		return len(v) == 1 && v[0] == "api"
	default:
		return false
	}
}

func migrateLegacyApiConfig(cfg map[string]interface{}) bool {
	api, ok := cfg["api"].(map[string]interface{})
	if !ok || !apiListenMissing(api) {
		return false
	}

	var listenAddr string
	inbounds, _ := cfg["inbounds"].([]interface{})

	apiInboundIndex := -1
	var apiInbound map[string]interface{}
	for i, item := range inbounds {
		inbound, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		tag, _ := inbound["tag"].(string)
		protocol, _ := inbound["protocol"].(string)
		if tag == "api" && protocol == "dokodemo-door" {
			apiInboundIndex = i
			apiInbound = inbound
			break
		}
	}

	if apiInboundIndex >= 0 {
		listenAddr = inboundListenAddress(apiInbound)
		if listenAddr == "" {
			listenAddr = defaultApiListen()
		}
		cfg["inbounds"] = removeIndex(inbounds, apiInboundIndex)

		if routing, ok := cfg["routing"].(map[string]interface{}); ok {
			if rawRules, ok := routing["rules"].([]interface{}); ok {
				for i, item := range rawRules {
					rule, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					if isLegacyApiRoutingRule(rule) {
						routing["rules"] = removeIndex(rawRules, i)
						cfg["routing"] = routing
						break
					}
				}
			}
		}
	} else {
		listenAddr = defaultApiListen()
	}

	api["listen"] = listenAddr
	cfg["api"] = api
	return true
}

func routingRuleFromMap(raw map[string]interface{}, index int) *model.RoutingRule {
	tag, _ := raw["ruleTag"].(string)
	delete(raw, "ruleTag")
	if tag == "" {
		tag = fmt.Sprintf("migrated-rule-%d", index)
	}
	b, _ := json.Marshal(raw)
	return &model.RoutingRule{
		Tag:     tag,
		Sort:    index,
		RawJson: string(b),
	}
}
