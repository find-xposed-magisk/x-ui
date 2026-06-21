package migrations

import (
	"github.com/alireza0/x-ui/logger"

	"gorm.io/gorm"
)

func migrateV006Settings(db *gorm.DB) error {
	db.Exec(`DELETE FROM settings WHERE key IN ('subJsonNoise','subJsonNoises', 'subJsonFragment')`)

	logger.Info("Migrated settings to v6")
	return nil
}
