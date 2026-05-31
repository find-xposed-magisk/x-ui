package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/xray"

	"gorm.io/gorm"
)

type RoutingRuleService struct {
	xrayApi        xray.XrayAPI
	settingService SettingService
}

func (s *RoutingRuleService) GetAllRules() ([]*model.RoutingRule, error) {
	db := database.GetDB()
	var rules []*model.RoutingRule
	err := db.Model(model.RoutingRule{}).Order("sort asc, id asc").Find(&rules).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return rules, nil
}

func (s *RoutingRuleService) GetRule(id int) (*model.RoutingRule, error) {
	db := database.GetDB()
	rule := &model.RoutingRule{}
	err := db.Model(model.RoutingRule{}).First(rule, id).Error
	if err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *RoutingRuleService) checkTagExist(tag string, ignoreId int) (bool, error) {
	db := database.GetDB().Model(model.RoutingRule{}).Where("tag = ?", tag)
	if ignoreId > 0 {
		db = db.Where("id != ?", ignoreId)
	}
	var count int64
	err := db.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *RoutingRuleService) ensureTag(rule *model.RoutingRule) {
	if rule.Tag != "" {
		return
	}
	if rule.Id > 0 {
		rule.Tag = fmt.Sprintf("rule-%d", rule.Id)
	} else {
		rule.Tag = fmt.Sprintf("rule-%d", time.Now().UnixNano()%1000000000)
	}
}

func (s *RoutingRuleService) AddRule(rule *model.RoutingRule) (*model.RoutingRule, bool, error) {
	s.ensureTag(rule)
	exist, err := s.checkTagExist(rule.Tag, 0)
	if err != nil {
		return rule, false, err
	}
	if exist {
		return rule, false, common.NewError("Tag already exists:", rule.Tag)
	}

	db := database.GetDB()
	var maxSort int
	db.Model(model.RoutingRule{}).Select("COALESCE(MAX(sort), -1)").Scan(&maxSort)
	rule.Sort = maxSort + 1

	err = db.Save(rule).Error
	if err != nil {
		return rule, false, err
	}

	needRestart := false
	if p != nil && p.IsRunning() {
		s.xrayApi.Init(p.GetAPIAddr())
		ruleJSON, err1 := rule.RuleJSON()
		if err1 != nil {
			logger.Debug("Unable to marshal routing rule:", err1)
			needRestart = true
		} else {
			err1 = s.xrayApi.AddRule(ruleJSON, true)
			if err1 == nil {
				logger.Debug("New routing rule added by api:", rule.Tag)
			} else {
				logger.Debug("Unable to add routing rule by api:", err1)
				needRestart = true
			}
		}
		s.xrayApi.Close()
	}

	return rule, needRestart, nil
}

func (s *RoutingRuleService) DelRule(id int) (bool, error) {
	db := database.GetDB()
	rule, err := s.GetRule(id)
	if err != nil {
		return false, err
	}

	needRestart := false
	if p != nil && p.IsRunning() {
		s.xrayApi.Init(p.GetAPIAddr())
		err1 := s.xrayApi.DelRule(rule.Tag)
		if err1 == nil {
			logger.Debug("Routing rule deleted by api:", rule.Tag)
		} else {
			logger.Debug("Unable to delete routing rule by api:", err1)
			needRestart = true
		}
		s.xrayApi.Close()
	}

	return needRestart, db.Delete(model.RoutingRule{}, id).Error
}

func (s *RoutingRuleService) UpdateRule(rule *model.RoutingRule) (*model.RoutingRule, bool, error) {
	exist, err := s.checkTagExist(rule.Tag, rule.Id)
	if err != nil {
		return rule, false, err
	}
	if exist {
		return rule, false, common.NewError("Tag already exists:", rule.Tag)
	}

	oldRule, err := s.GetRule(rule.Id)
	if err != nil {
		return rule, false, err
	}

	oldTag := oldRule.Tag
	oldRule.Tag = rule.Tag
	oldRule.RawJson = rule.RawJson

	needRestart := false
	if p != nil && p.IsRunning() {
		s.xrayApi.Init(p.GetAPIAddr())
		if s.xrayApi.DelRule(oldTag) != nil {
			needRestart = true
		}
		ruleJSON, err2 := oldRule.RuleJSON()
		if err2 != nil {
			needRestart = true
		} else {
			err2 = s.xrayApi.AddRule(ruleJSON, true)
			if err2 != nil {
				logger.Debug("Unable to update routing rule by api:", err2)
				needRestart = true
			}
		}
		s.xrayApi.Close()
	}

	db := database.GetDB()
	return oldRule, needRestart, db.Save(oldRule).Error
}

func (s *RoutingRuleService) ReorderRules(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	db := database.GetDB()
	return db.Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			if err := tx.Model(model.RoutingRule{}).Where("id = ?", id).Update("sort", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *RoutingRuleService) GetRoutingMeta() (map[string]interface{}, error) {
	templateConfig, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(templateConfig), &cfg); err != nil {
		return nil, err
	}
	routing, _ := cfg["routing"].(map[string]interface{})
	meta := map[string]interface{}{
		"domainStrategy": "AsIs",
		"balancers":      []interface{}{},
	}
	if routing == nil {
		return meta, nil
	}
	if ds, ok := routing["domainStrategy"].(string); ok {
		meta["domainStrategy"] = ds
	}
	if balancers, ok := routing["balancers"]; ok {
		meta["balancers"] = balancers
	}
	return meta, nil
}

func (s *RoutingRuleService) SaveRoutingMeta(domainStrategy string) error {
	templateConfig, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(templateConfig), &cfg); err != nil {
		return err
	}
	routing, ok := cfg["routing"].(map[string]interface{})
	if !ok || routing == nil {
		routing = map[string]interface{}{}
		cfg["routing"] = routing
	}
	routing["domainStrategy"] = domainStrategy
	routing["rules"] = []interface{}{}
	newTemplate, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return s.settingService.saveSetting("xrayTemplateConfig", string(newTemplate))
}

func (s *RoutingRuleService) SyncBasicProperty(outboundTag, property string, data []string) error {
	rules, err := s.GetAllRules()
	if err != nil {
		return err
	}
	var target *model.RoutingRule
	for _, r := range rules {
		var raw map[string]interface{}
		if json.Unmarshal([]byte(r.RawJson), &raw) != nil {
			continue
		}
		ot, _ := raw["outboundTag"].(string)
		if ot != outboundTag {
			continue
		}
		if _, has := raw[property]; has {
			target = r
			break
		}
	}
	if target == nil {
		if len(data) == 0 {
			return nil
		}
		ruleObj := map[string]interface{}{
			"type":        "field",
			"outboundTag": outboundTag,
			property:      data,
		}
		raw, _ := json.Marshal(ruleObj)
		newRule := &model.RoutingRule{RawJson: string(raw)}
		_, needRestart, err := s.AddRule(newRule)
		if needRestart {
			isNeedXrayRestart.Store(true)
		}
		return err
	}
	if len(data) == 0 {
		needRestart, err := s.DelRule(target.Id)
		if needRestart {
			isNeedXrayRestart.Store(true)
		}
		return err
	}
	var raw map[string]interface{}
	json.Unmarshal([]byte(target.RawJson), &raw)
	raw[property] = data
	newRaw, _ := json.Marshal(raw)
	target.RawJson = string(newRaw)
	_, needRestart, err := s.UpdateRule(target)
	if needRestart {
		isNeedXrayRestart.Store(true)
	}
	return err
}

func (s *RoutingRuleService) GetBasicProperty(outboundTag, property string) ([]string, error) {
	rules, err := s.GetAllRules()
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		var raw map[string]interface{}
		if json.Unmarshal([]byte(r.RawJson), &raw) != nil {
			continue
		}
		ot, _ := raw["outboundTag"].(string)
		if ot != outboundTag {
			continue
		}
		if val, ok := raw[property]; ok {
			switch v := val.(type) {
			case []interface{}:
				result := make([]string, 0, len(v))
				for _, item := range v {
					if s, ok := item.(string); ok {
						result = append(result, s)
					}
				}
				return result, nil
			case []string:
				return v, nil
			}
		}
	}
	return []string{}, nil
}

func (s *RoutingRuleService) ReplaceBalancerTag(oldTag, newTag string) error {
	rules, err := s.GetAllRules()
	if err != nil {
		return err
	}
	needRestart := false
	for _, r := range rules {
		var raw map[string]interface{}
		if json.Unmarshal([]byte(r.RawJson), &raw) != nil {
			continue
		}
		bt, _ := raw["balancerTag"].(string)
		if bt != oldTag {
			continue
		}
		raw["balancerTag"] = newTag
		newRaw, _ := json.Marshal(raw)
		r.RawJson = string(newRaw)
		_, nr, err := s.UpdateRule(r)
		if err != nil {
			return err
		}
		if nr {
			needRestart = true
		}
	}
	if needRestart {
		isNeedXrayRestart.Store(true)
	}
	return nil
}

func apiListenMissing(api map[string]interface{}) bool {
	_, ok := api["listen"]
	return !ok
}

func defaultApiListen() string {
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(xrayTemplateConfig), &cfg); err != nil {
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
		cfg["inbounds"] = RemoveIndex(inbounds, apiInboundIndex)

		if routing, ok := cfg["routing"].(map[string]interface{}); ok {
			if rawRules, ok := routing["rules"].([]interface{}); ok {
				for i, item := range rawRules {
					rule, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					if isLegacyApiRoutingRule(rule) {
						routing["rules"] = RemoveIndex(rawRules, i)
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

func (s *RoutingRuleService) MigrateDB() {
	db := database.GetDB()
	var count int64
	db.Model(&model.RoutingRule{}).Count(&count)

	templateConfig, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		logger.Warning("routing rule migration: get xray template failed:", err)
		return
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(templateConfig), &cfg); err != nil {
		logger.Warning("routing rule migration: parse template failed:", err)
		return
	}

	if migrateLegacyApiConfig(cfg) {
		newTemplate, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			logger.Warning("routing rule migration: marshal legacy api config failed:", err)
			return
		}
		if err := s.settingService.saveSetting("xrayTemplateConfig", string(newTemplate)); err != nil {
			logger.Warning("routing rule migration: save legacy api config failed:", err)
			return
		}
		logger.Info("Migrated legacy api inbound to api.listen in xray settings")
	}

	if count > 0 {
		return
	}

	routing, ok := cfg["routing"].(map[string]interface{})
	if !ok {
		return
	}
	rawRules, ok := routing["rules"].([]interface{})
	if !ok || len(rawRules) == 0 {
		return
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
		return
	}
	if err := s.settingService.saveSetting("xrayTemplateConfig", string(newTemplate)); err != nil {
		logger.Warning("routing rule migration: save template failed:", err)
		return
	}
	logger.Info("Migrated", len(rawRules), "routing rule(s) from xray settings to database")
}

func (s *RoutingRuleService) BuildRulesArray() ([]interface{}, error) {
	rules, err := s.GetAllRules()
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, 0, len(rules))
	for _, r := range rules {
		ruleJSON, err := r.RuleJSON()
		if err != nil {
			continue
		}
		var obj interface{}
		if json.Unmarshal(ruleJSON, &obj) == nil {
			result = append(result, obj)
		}
	}
	return result, nil
}
