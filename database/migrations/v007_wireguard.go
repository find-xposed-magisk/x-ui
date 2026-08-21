package migrations

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/util/random"
	"github.com/alireza0/x-ui/xray"

	"gorm.io/gorm"
)

// migrateV007Wireguard turns existing WireGuard inbounds into regular
// multi-user inbounds. Xray-core used to treat a WireGuard inbound as a single
// opaque tunnel, so the panel stored its peers under "peers" with no identity of
// their own. Now that the core attributes every peer to a named user, peers move
// under "clients" like every other multi-user protocol and gain an email plus
// the usual quota/expiry bookkeeping.
func migrateV007Wireguard(db *gorm.DB) error {
	tx := db.Begin()
	var err error
	defer func() {
		if err == nil {
			tx.Commit()
		} else {
			tx.Rollback()
		}
	}()

	var inbounds []*model.Inbound
	err = tx.Model(model.Inbound{}).Where("protocol = ?", string(model.Wireguard)).Find(&inbounds).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if len(inbounds) == 0 {
		err = nil
		return nil
	}

	takenEmails, err := existingEmails(tx)
	if err != nil {
		return err
	}

	for _, inbound := range inbounds {
		settings := map[string]interface{}{}
		if json.Unmarshal([]byte(inbound.Settings), &settings) != nil {
			continue
		}
		if _, done := settings["clients"]; done {
			continue
		}
		peers, ok := settings["peers"].([]interface{})
		if !ok {
			continue
		}

		for index, peer := range peers {
			p, ok := peer.(map[string]interface{})
			if !ok {
				continue
			}
			email, _ := p["email"].(string)
			if email == "" {
				email = uniqueEmail(takenEmails, fmt.Sprintf("%s-peer%d", inbound.Tag, index+1))
				p["email"] = email
			}
			takenEmails[strings.ToLower(email)] = struct{}{}

			setIfMissing(p, "enable", true)
			setIfMissing(p, "totalGB", 0)
			setIfMissing(p, "expiryTime", 0)
			setIfMissing(p, "limitIp", 0)
			setIfMissing(p, "reset", 0)
			setIfMissing(p, "tgId", "")
			setIfMissing(p, "subId", random.Seq(16))
		}

		settings["clients"] = peers
		delete(settings, "peers")

		var modified []byte
		modified, err = json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return err
		}
		inbound.Settings = string(modified)

		err = tx.Model(model.Inbound{}).Where("id = ?", inbound.Id).Update("settings", inbound.Settings).Error
		if err != nil {
			return err
		}

		for _, client := range parseInboundClients(inbound.Settings) {
			if client.Email == "" {
				continue
			}
			var count int64
			tx.Model(xray.ClientTraffic{}).Where("email = ?", client.Email).Count(&count)
			if count > 0 {
				continue
			}
			if err = addClientStat(tx, inbound.Id, &client); err != nil {
				return err
			}
		}
	}

	return nil
}

// existingEmails collects every email already in use, so generated peer emails
// cannot collide with a client of another inbound. The settings column is read
// in Go rather than with SQLite's JSON functions because those abort the whole
// statement — and with it the upgrade — on the first row that does not hold
// valid JSON, and nothing has ever stopped an inbound from storing one.
func existingEmails(tx *gorm.DB) (map[string]struct{}, error) {
	var settings []string
	if err := tx.Model(model.Inbound{}).Pluck("settings", &settings).Error; err != nil {
		return nil, err
	}

	var stats []string
	if err := tx.Model(xray.ClientTraffic{}).Distinct().Pluck("email", &stats).Error; err != nil {
		return nil, err
	}

	taken := make(map[string]struct{}, len(stats))
	for _, entry := range settings {
		for _, client := range parseInboundClients(entry) {
			if client.Email != "" {
				taken[strings.ToLower(client.Email)] = struct{}{}
			}
		}
	}
	for _, email := range stats {
		if email != "" {
			taken[strings.ToLower(email)] = struct{}{}
		}
	}
	return taken, nil
}

func uniqueEmail(taken map[string]struct{}, base string) string {
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := taken[strings.ToLower(candidate)]; !exists {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func setIfMissing(client map[string]interface{}, key string, value interface{}) {
	if _, ok := client[key]; !ok {
		client[key] = value
	}
}
