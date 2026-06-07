package migrations

import (
	"encoding/json"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/xray"

	"gorm.io/gorm"
)

func migrateV004Inbound(db *gorm.DB) error {
	if err := migrationInboundRequirements(db); err != nil {
		return err
	}
	migrationRemoveOrphanedTraffics(db)
	return nil
}

func migrationInboundRequirements(db *gorm.DB) error {
	tx := db.Begin()
	var err error
	defer func() {
		if err == nil {
			tx.Commit()
			if dbErr := db.Exec(`VACUUM "main"`).Error; dbErr != nil {
				logger.Warningf("VACUUM failed: %v", dbErr)
			}
		} else {
			tx.Rollback()
		}
	}()

	var inbounds []*model.Inbound
	err = tx.Model(model.Inbound{}).Where("protocol IN (?)", []string{"vmess", "vless", "trojan"}).Find(&inbounds).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	for inboundIndex := range inbounds {
		settings := map[string]interface{}{}
		json.Unmarshal([]byte(inbounds[inboundIndex].Settings), &settings)
		clients, ok := settings["clients"].([]interface{})
		if ok {
			var newClients []interface{}
			for clientIndex := range clients {
				c := clients[clientIndex].(map[string]interface{})

				if _, ok := c["email"]; !ok {
					c["email"] = ""
				}

				if _, ok := c["flow"]; ok {
					if c["flow"] == "xtls-rprx-direct" {
						c["flow"] = ""
					}
				}
				newClients = append(newClients, interface{}(c))
			}
			settings["clients"] = newClients
			modifiedSettings, marshalErr := json.MarshalIndent(settings, "", "  ")
			if marshalErr != nil {
				err = marshalErr
				return err
			}

			inbounds[inboundIndex].Settings = string(modifiedSettings)
		}

		modelClients := parseInboundClients(inbounds[inboundIndex].Settings)
		for _, modelClient := range modelClients {
			if len(modelClient.Email) > 0 {
				var count int64
				tx.Model(xray.ClientTraffic{}).Where("email = ?", modelClient.Email).Count(&count)
				if count == 0 {
					if addErr := addClientStat(tx, inbounds[inboundIndex].Id, &modelClient); addErr != nil {
						err = addErr
						return err
					}
				}
			}
		}
	}
	if len(inbounds) > 0 {
		if saveErr := tx.Save(inbounds).Error; saveErr != nil {
			err = saveErr
			return err
		}
	}

	tx.Where("inbound_id = 0").Delete(xray.ClientTraffic{})

	var externalProxy []struct {
		Id             int
		Port           int
		StreamSettings []byte
	}
	err = tx.Raw(`select id, port, stream_settings
	from inbounds
	WHERE protocol in ('vmess','vless','trojan')
	  AND json_extract(stream_settings, '$.security') = 'tls'
	  AND json_extract(stream_settings, '$.tlsSettings.settings.domains') IS NOT NULL`).Scan(&externalProxy).Error
	if err != nil || len(externalProxy) == 0 {
		return nil
	}

	for _, ep := range externalProxy {
		var reverses interface{}
		var stream map[string]interface{}
		json.Unmarshal(ep.StreamSettings, &stream)
		if tlsSettings, ok := stream["tlsSettings"].(map[string]interface{}); ok {
			if settings, ok := tlsSettings["settings"].(map[string]interface{}); ok {
				if domains, ok := settings["domains"].([]interface{}); ok {
					for _, domain := range domains {
						if domainMap, ok := domain.(map[string]interface{}); ok {
							domainMap["forceTls"] = "same"
							domainMap["port"] = ep.Port
							domainMap["dest"] = domainMap["domain"].(string)
							delete(domainMap, "domain")
						}
					}
				}
				reverses = settings["domains"]
				delete(settings, "domains")
			}
		}
		stream["externalProxy"] = reverses
		newStream, _ := json.MarshalIndent(stream, " ", "  ")
		tx.Model(model.Inbound{}).Where("id = ?", ep.Id).Update("stream_settings", newStream)
	}
	return nil
}

func migrationRemoveOrphanedTraffics(db *gorm.DB) {
	db.Exec(`
		DELETE FROM client_traffics
		WHERE email NOT IN (
			SELECT JSON_EXTRACT(client.value, '$.email')
			FROM inbounds,
				JSON_EACH(JSON_EXTRACT(inbounds.settings, '$.clients')) AS client
		)
	`)
}

func parseInboundClients(settingsJSON string) []model.Client {
	settings := map[string]interface{}{}
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		return nil
	}
	raw, ok := settings["clients"]
	if !ok || raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case []interface{}:
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var clients []model.Client
		if json.Unmarshal(b, &clients) != nil {
			return nil
		}
		return clients
	case string:
		if v == "" {
			return nil
		}
		var clients []model.Client
		if json.Unmarshal([]byte(v), &clients) != nil {
			return nil
		}
		return clients
	default:
		return nil
	}
}

func addClientStat(tx *gorm.DB, inboundId int, client *model.Client) error {
	clientTraffic := xray.ClientTraffic{
		InboundId:  inboundId,
		Email:      client.Email,
		Total:      client.TotalGB,
		ExpiryTime: client.ExpiryTime,
		Enable:     client.Enable,
		Up:         0,
		Down:       0,
		Reset:      client.Reset,
	}
	return tx.Create(&clientTraffic).Error
}
