package migrations

import (
	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/database/model"

	"gorm.io/gorm"
)

const xrayTemplateConfigKey = "xrayTemplateConfig"

func getXrayTemplate(db *gorm.DB) (string, error) {
	setting := &model.Setting{}
	err := db.Model(model.Setting{}).Where("key = ?", xrayTemplateConfigKey).First(setting).Error
	if err == gorm.ErrRecordNotFound {
		return config.GetDefaultXrayTemplate(), nil
	}
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func saveXrayTemplate(db *gorm.DB, value string) error {
	setting := &model.Setting{}
	err := db.Model(model.Setting{}).Where("key = ?", xrayTemplateConfigKey).First(setting).Error
	if err == gorm.ErrRecordNotFound {
		return db.Create(&model.Setting{
			Key:   xrayTemplateConfigKey,
			Value: value,
		}).Error
	}
	if err != nil {
		return err
	}
	setting.Value = value
	return db.Save(setting).Error
}
