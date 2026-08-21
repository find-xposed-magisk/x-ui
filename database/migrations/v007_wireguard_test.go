package migrations

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/xray"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Name the database after the test so a leftover connection from another
	// test can never leak rows into this one.
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("unwrap db: %v", err)
	}
	// A shared-cache memory database lives as long as one connection is open,
	// so pin the pool to a single one and close it with the test.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })

	if err := db.AutoMigrate(&model.Inbound{}, &xray.ClientTraffic{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func settingsOf(t *testing.T, db *gorm.DB, id int) map[string]interface{} {
	t.Helper()
	var inbound model.Inbound
	if err := db.First(&inbound, id).Error; err != nil {
		t.Fatalf("reload inbound %d: %v", id, err)
	}
	settings := map[string]interface{}{}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		t.Fatalf("inbound %d settings are not JSON: %v", id, err)
	}
	return settings
}

func clientsOf(t *testing.T, db *gorm.DB, id int) []interface{} {
	t.Helper()
	settings := settingsOf(t, db, id)
	if _, stale := settings["peers"]; stale {
		t.Fatalf("inbound %d still stores peers", id)
	}
	clients, ok := settings["clients"].([]interface{})
	if !ok {
		t.Fatalf("inbound %d has no clients array: %v", id, settings)
	}
	return clients
}

func TestMigrateV007MovesPeersToClients(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&model.Inbound{
		Id: 1, UserId: 1, Tag: "inbound-wg", Protocol: model.Wireguard, Port: 51820, Enable: true,
		Settings: `{"secretKey":"key","peers":[
			{"privateKey":"p1","publicKey":"k1","allowedIPs":["10.0.0.2/32"]},
			{"privateKey":"p2","publicKey":"k2","allowedIPs":["10.0.0.3/32"],"email":"named"}
		]}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := migrateV007Wireguard(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	clients := clientsOf(t, db, 1)
	if len(clients) != 2 {
		t.Fatalf("got %d clients, want 2", len(clients))
	}

	generated := clients[0].(map[string]interface{})
	if generated["email"] != "inbound-wg-peer1" {
		t.Fatalf("generated email = %v, want inbound-wg-peer1", generated["email"])
	}
	if generated["publicKey"] != "k1" || generated["privateKey"] != "p1" {
		t.Fatalf("peer credentials were not preserved: %v", generated)
	}
	for _, key := range []string{"enable", "totalGB", "expiryTime", "limitIp", "reset", "tgId", "subId"} {
		if _, ok := generated[key]; !ok {
			t.Fatalf("peer is missing the %q bookkeeping field: %v", key, generated)
		}
	}
	if generated["enable"] != true {
		t.Fatalf("migrated peer should start enabled, got %v", generated["enable"])
	}
	if subId, _ := generated["subId"].(string); len(subId) != 16 {
		t.Fatalf("subId = %q, want 16 generated characters", subId)
	}

	named := clients[1].(map[string]interface{})
	if named["email"] != "named" {
		t.Fatalf("an existing email was overwritten: %v", named["email"])
	}

	var traffics []xray.ClientTraffic
	if err := db.Find(&traffics).Error; err != nil {
		t.Fatalf("load traffics: %v", err)
	}
	if len(traffics) != 2 {
		t.Fatalf("got %d traffic rows, want one per peer", len(traffics))
	}
	for _, traffic := range traffics {
		if traffic.InboundId != 1 {
			t.Fatalf("traffic %q is attached to inbound %d", traffic.Email, traffic.InboundId)
		}
	}
}

// An inbound whose settings are not valid JSON used to abort the whole upgrade,
// because the email survey ran as a SQLite JSON query over every row.
func TestMigrateV007SurvivesMalformedSettings(t *testing.T) {
	db := newTestDB(t)
	seed := []*model.Inbound{
		{Id: 1, UserId: 1, Tag: "broken", Protocol: model.Protocol("dokodemo-door"), Port: 53, Settings: "not json"},
		{Id: 2, UserId: 1, Tag: "empty", Protocol: model.Protocol("socks"), Port: 1080, Settings: ""},
		{Id: 3, UserId: 1, Tag: "no-clients", Protocol: model.Protocol("dokodemo-door"), Port: 54, Settings: `{"address":"1.1.1.1"}`},
		{Id: 4, UserId: 1, Tag: "wg", Protocol: model.Wireguard, Port: 51820,
			Settings: `{"secretKey":"key","peers":[{"publicKey":"k1","allowedIPs":["10.0.0.2/32"]}]}`},
	}
	for _, inbound := range seed {
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("seed %s: %v", inbound.Tag, err)
		}
	}

	if err := migrateV007Wireguard(db); err != nil {
		t.Fatalf("migration failed on a database holding malformed settings: %v", err)
	}

	clients := clientsOf(t, db, 4)
	if len(clients) != 1 {
		t.Fatalf("got %d clients, want 1", len(clients))
	}
	if email := clients[0].(map[string]interface{})["email"]; email != "wg-peer1" {
		t.Fatalf("email = %v, want wg-peer1", email)
	}
}

func TestMigrateV007AvoidsEmailCollisions(t *testing.T) {
	db := newTestDB(t)
	seed := []*model.Inbound{
		// Another inbound already owns the name the peer would be given.
		{Id: 1, UserId: 1, Tag: "other", Protocol: model.Protocol("vless"), Port: 443,
			Settings: `{"clients":[{"id":"uuid","email":"WG-PEER1"}]}`},
		{Id: 2, UserId: 1, Tag: "wg", Protocol: model.Wireguard, Port: 51820,
			Settings: `{"peers":[{"publicKey":"k1"},{"publicKey":"k2"}]}`},
	}
	for _, inbound := range seed {
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("seed %s: %v", inbound.Tag, err)
		}
	}
	// ...and a stale traffic row owns the next candidate.
	if err := db.Create(&xray.ClientTraffic{InboundId: 1, Email: "wg-peer1-2"}).Error; err != nil {
		t.Fatalf("seed traffic: %v", err)
	}

	if err := migrateV007Wireguard(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	clients := clientsOf(t, db, 2)
	first := clients[0].(map[string]interface{})["email"].(string)
	second := clients[1].(map[string]interface{})["email"].(string)
	if strings.EqualFold(first, "wg-peer1") || strings.EqualFold(first, "wg-peer1-2") {
		t.Fatalf("first peer took a taken email: %q", first)
	}
	if strings.EqualFold(first, second) {
		t.Fatalf("both peers were given the same email: %q", first)
	}
}

func TestMigrateV007IsIdempotent(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&model.Inbound{
		Id: 1, UserId: 1, Tag: "wg", Protocol: model.Wireguard, Port: 51820,
		Settings: `{"peers":[{"publicKey":"k1","allowedIPs":["10.0.0.2/32"]}]}`,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := migrateV007Wireguard(db); err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstPass := settingsOf(t, db, 1)

	if err := migrateV007Wireguard(db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := settingsOf(t, db, 1); !jsonEqual(t, firstPass, got) {
		t.Fatalf("a second run changed the settings:\nfirst:  %v\nsecond: %v", firstPass, got)
	}

	var count int64
	if err := db.Model(&xray.ClientTraffic{}).Count(&count).Error; err != nil {
		t.Fatalf("count traffics: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d traffic rows after two runs, want 1", count)
	}
}

func TestMigrateV007LeavesOtherProtocolsAlone(t *testing.T) {
	db := newTestDB(t)
	original := `{"clients":[{"id":"uuid","email":"vless-user"}]}`
	if err := db.Create(&model.Inbound{
		Id: 1, UserId: 1, Tag: "vl", Protocol: model.Protocol("vless"), Port: 443, Settings: original,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := migrateV007Wireguard(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	var inbound model.Inbound
	if err := db.First(&inbound, 1).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if inbound.Settings != original {
		t.Fatalf("settings = %q, want them untouched", inbound.Settings)
	}
	var count int64
	db.Model(&xray.ClientTraffic{}).Count(&count)
	if count != 0 {
		t.Fatalf("got %d traffic rows, want none created for a non-wireguard inbound", count)
	}
}

func TestUniqueEmail(t *testing.T) {
	taken := map[string]struct{}{"base": {}, "base-2": {}}
	if got := uniqueEmail(taken, "base"); got != "base-3" {
		t.Fatalf("uniqueEmail = %q, want base-3", got)
	}
	if got := uniqueEmail(taken, "fresh"); got != "fresh" {
		t.Fatalf("uniqueEmail = %q, want fresh", got)
	}
	// Emails are compared case-insensitively so the two cannot collide in the UI.
	if got := uniqueEmail(map[string]struct{}{"base": {}}, "BASE"); got == "BASE" {
		t.Fatal("uniqueEmail ignored case when checking for a collision")
	}
}

func TestSetIfMissing(t *testing.T) {
	client := map[string]interface{}{"enable": false}
	setIfMissing(client, "enable", true)
	setIfMissing(client, "totalGB", 0)
	if client["enable"] != false {
		t.Fatal("setIfMissing overwrote an existing value")
	}
	if client["totalGB"] != 0 {
		t.Fatal("setIfMissing did not add the absent key")
	}
}

func jsonEqual(t *testing.T, a, b interface{}) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(left) == string(right)
}
