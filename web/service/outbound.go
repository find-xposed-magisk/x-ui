package service

import (
	"encoding/json"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/xray"

	"gorm.io/gorm"
)

type OutboundService struct {
	xrayApi        xray.XrayAPI
	settingService SettingService
}

func (s *OutboundService) GetAllOutbounds() ([]*model.Outbound, error) {
	db := database.GetDB()
	var outbounds []*model.Outbound
	err := db.Model(model.Outbound{}).Order("sort asc, id asc").Find(&outbounds).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return outbounds, nil
}

func (s *OutboundService) GetOutbound(id int) (*model.Outbound, error) {
	db := database.GetDB()
	outbound := &model.Outbound{}
	err := db.Model(model.Outbound{}).First(outbound, id).Error
	if err != nil {
		return nil, err
	}
	return outbound, nil
}

func (s *OutboundService) checkTagExist(tag string, ignoreId int) (bool, error) {
	db := database.GetDB().Model(model.Outbound{}).Where("tag = ?", tag)
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

func (s *OutboundService) AddOutbound(outbound *model.Outbound) (*model.Outbound, bool, error) {
	exist, err := s.checkTagExist(outbound.Tag, 0)
	if err != nil {
		return outbound, false, err
	}
	if exist {
		return outbound, false, common.NewError("Tag already exists:", outbound.Tag)
	}

	db := database.GetDB()
	var maxSort int
	db.Model(model.Outbound{}).Select("COALESCE(MAX(sort), -1)").Scan(&maxSort)
	outbound.Sort = maxSort + 1

	err = db.Save(outbound).Error
	if err != nil {
		return outbound, false, err
	}

	needRestart := false
	if p != nil && p.IsRunning() {
		s.xrayApi.Init(p.GetAPIAddr())
		outboundJson, err1 := json.MarshalIndent(outbound.GenXrayOutboundConfig(), "", "  ")
		if err1 != nil {
			logger.Debug("Unable to marshal outbound config:", err1)
		} else {
			err1 = s.xrayApi.AddOutbound(outboundJson)
			if err1 == nil {
				logger.Debug("New outbound added by api:", outbound.Tag)
			} else {
				logger.Debug("Unable to add outbound by api:", err1)
				needRestart = true
			}
		}
		s.xrayApi.Close()
	}

	return outbound, needRestart, nil
}

func (s *OutboundService) DelOutbound(id int) (bool, error) {
	db := database.GetDB()
	outbound, err := s.GetOutbound(id)
	if err != nil {
		return false, err
	}

	needRestart := false
	if p != nil && p.IsRunning() {
		s.xrayApi.Init(p.GetAPIAddr())
		err1 := s.xrayApi.DelOutbound(outbound.Tag)
		if err1 == nil {
			logger.Debug("Outbound deleted by api:", outbound.Tag)
		} else {
			logger.Debug("Unable to delete outbound by api:", err1)
			needRestart = true
		}
		s.xrayApi.Close()
	}

	return needRestart, db.Delete(model.Outbound{}, id).Error
}

func (s *OutboundService) UpdateOutbound(outbound *model.Outbound) (*model.Outbound, bool, error) {
	exist, err := s.checkTagExist(outbound.Tag, outbound.Id)
	if err != nil {
		return outbound, false, err
	}
	if exist {
		return outbound, false, common.NewError("Tag already exists:", outbound.Tag)
	}

	oldOutbound, err := s.GetOutbound(outbound.Id)
	if err != nil {
		return outbound, false, err
	}

	oldTag := oldOutbound.Tag
	oldOutbound.SendThrough = outbound.SendThrough
	oldOutbound.Protocol = outbound.Protocol
	oldOutbound.Settings = outbound.Settings
	oldOutbound.Tag = outbound.Tag
	oldOutbound.StreamSettings = outbound.StreamSettings
	oldOutbound.ProxySettings = outbound.ProxySettings
	oldOutbound.Mux = outbound.Mux
	oldOutbound.TargetStrategy = outbound.TargetStrategy

	needRestart := false
	if p != nil && p.IsRunning() {
		s.xrayApi.Init(p.GetAPIAddr())
		if s.xrayApi.DelOutbound(oldTag) == nil {
			logger.Debug("Old outbound deleted by api:", oldTag)
		}
		outboundJson, err2 := json.MarshalIndent(oldOutbound.GenXrayOutboundConfig(), "", "  ")
		if err2 != nil {
			logger.Debug("Unable to marshal updated outbound config:", err2)
			needRestart = true
		} else {
			err2 = s.xrayApi.AddOutbound(outboundJson)
			if err2 == nil {
				logger.Debug("Updated outbound added by api:", oldOutbound.Tag)
			} else {
				logger.Debug("Unable to update outbound by api:", err2)
				needRestart = true
			}
		}
		s.xrayApi.Close()
	}

	db := database.GetDB()
	return outbound, needRestart, db.Save(oldOutbound).Error
}

func (s *OutboundService) SetFirstOutbound(id int) error {
	db := database.GetDB()
	outbound, err := s.GetOutbound(id)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(model.Outbound{}).Where("sort < ?", outbound.Sort).
			Update("sort", gorm.Expr("sort + 1")).Error; err != nil {
			return err
		}
		return tx.Model(model.Outbound{}).Where("id = ?", id).Update("sort", 0).Error
	})
}

func (s *OutboundService) ResetTraffic(id int) error {
	db := database.GetDB()
	return db.Model(model.Outbound{}).Where("id = ?", id).
		Updates(map[string]interface{}{"up": 0, "down": 0}).Error
}

func (s *OutboundService) ResetAllTraffics() error {
	db := database.GetDB()
	return db.Model(model.Outbound{}).Where("1 = 1").
		Updates(map[string]interface{}{"up": 0, "down": 0}).Error
}

func (s *OutboundService) GetOutboundSummariesJSON() (string, error) {
	outbounds, err := s.GetAllOutbounds()
	if err != nil {
		return "", err
	}
	summaries := make([]map[string]interface{}, 0, len(outbounds))
	for _, o := range outbounds {
		var settings interface{}
		if len(o.Settings) > 0 {
			json.Unmarshal([]byte(o.Settings), &settings)
		}
		summaries = append(summaries, map[string]interface{}{
			"id":       o.Id,
			"tag":      o.Tag,
			"protocol": o.Protocol,
			"settings": settings,
		})
	}
	result, _ := json.Marshal(summaries)
	return string(result), nil
}

func (s *OutboundService) GetOutboundReverseTags() (string, error) {
	outbounds, err := s.GetAllOutbounds()
	if err != nil {
		return "", err
	}
	var tags []string
	for _, o := range outbounds {
		if o.Protocol != "vless" {
			continue
		}
		var settings map[string]interface{}
		if json.Unmarshal([]byte(o.Settings), &settings) != nil {
			continue
		}
		reverse, ok := settings["reverse"].(map[string]interface{})
		if !ok {
			continue
		}
		if tag, ok := reverse["tag"].(string); ok && tag != "" {
			tags = append(tags, tag)
		}
	}
	result, _ := json.Marshal(tags)
	return string(result), nil
}

func (s *OutboundService) GetOutboundTags() (string, error) {
	db := database.GetDB()
	var tags []string
	err := db.Model(model.Outbound{}).Select("tag").Order("sort asc, id asc").Find(&tags).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return "", err
	}
	result, _ := json.Marshal(tags)
	return string(result), nil
}

func (s *OutboundService) AddTraffic(traffics []*xray.Traffic) error {
	hasOutboundTraffic := false
	for _, traffic := range traffics {
		if !traffic.IsInbound {
			hasOutboundTraffic = true
			break
		}
	}
	if !hasOutboundTraffic {
		if p != nil {
			p.SetOnlineOutbounds(nil)
		}
		return nil
	}

	var onlineOutbounds []string
	db := database.GetDB()
	for _, traffic := range traffics {
		if traffic.IsInbound {
			continue
		}
		err := db.Model(&model.Outbound{}).Where("tag = ?", traffic.Tag).
			Updates(map[string]interface{}{
				"up":   gorm.Expr("up + ?", traffic.Up),
				"down": gorm.Expr("down + ?", traffic.Down),
			}).Error
		if err != nil {
			return err
		}
		if traffic.Up+traffic.Down > 0 {
			onlineOutbounds = append(onlineOutbounds, traffic.Tag)
		}
	}
	if p != nil {
		p.SetOnlineOutbounds(onlineOutbounds)
	}
	return nil
}

func (s *OutboundService) GetOnlineOutbounds() []string {
	if p == nil {
		return nil
	}
	return p.GetOnlineOutbounds()
}
