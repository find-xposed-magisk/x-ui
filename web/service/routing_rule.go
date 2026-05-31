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
