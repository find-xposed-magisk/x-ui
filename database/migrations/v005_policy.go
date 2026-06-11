package migrations

import (
	"encoding/json"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/logger"

	"gorm.io/gorm"
)

func migrateV005Policy(db *gorm.DB) error {
	templateConfig, err := getXrayTemplate(db)
	if err != nil {
		logger.Warning("policy migration: get xray template failed:", err)
		return nil
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(templateConfig), &cfg); err != nil {
		logger.Warning("policy migration: parse template failed:", err)
		return nil
	}

	var defaultCfg map[string]interface{}
	if err := json.Unmarshal([]byte(config.GetDefaultXrayTemplate()), &defaultCfg); err != nil {
		logger.Warning("policy migration: parse default template failed:", err)
		return nil
	}

	defaultPolicy, ok := defaultCfg["policy"]
	if !ok {
		return nil
	}

	cfg["policy"] = defaultPolicy

	newTemplate, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		logger.Warning("policy migration: marshal template failed:", err)
		return nil
	}

	if err := saveXrayTemplate(db, string(newTemplate)); err != nil {
		logger.Warning("policy migration: save template failed:", err)
		return nil
	}

	logger.Info("Migrated policy in xray settings to match default config")
	return nil
}
