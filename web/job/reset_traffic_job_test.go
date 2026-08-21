package job

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/util/cronspec"
	"github.com/alireza0/x-ui/web/service"
	"github.com/alireza0/x-ui/xray"
)

func newTestDatabase(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
}

// seedClients writes two clients: one that ran out of traffic and was disabled,
// and one still running.
func seedClients(t *testing.T) {
	t.Helper()
	db := database.GetDB()
	clients := []xray.ClientTraffic{
		{InboundId: 1, Email: "depleted@example.com", Enable: false, Up: 60, Down: 40, Total: 100},
		{InboundId: 1, Email: "active@example.com", Enable: true, Up: 5, Down: 5, Total: 100},
	}
	for i := range clients {
		if err := db.Create(&clients[i]).Error; err != nil {
			t.Fatalf("seed %s: %v", clients[i].Email, err)
		}
	}
}

func loadClient(t *testing.T, email string) xray.ClientTraffic {
	t.Helper()
	var client xray.ClientTraffic
	if err := database.GetDB().Where("email = ?", email).First(&client).Error; err != nil {
		t.Fatalf("load %s: %v", email, err)
	}
	return client
}

func newResetJob(t *testing.T, spec string) *ResetTrafficJob {
	t.Helper()
	schedule, err := cronspec.Parse(spec)
	if err != nil {
		t.Fatalf("parse %q: %v", spec, err)
	}
	if schedule == nil {
		t.Fatalf("%q parsed as disabled", spec)
	}
	return NewResetTrafficJob(schedule)
}

func TestResetTrafficJobZeroesAndReEnables(t *testing.T) {
	newTestDatabase(t)
	seedClients(t)

	newResetJob(t, "@daily").Run()

	depleted := loadClient(t, "depleted@example.com")
	if depleted.Up != 0 || depleted.Down != 0 {
		t.Errorf("depleted client still has %d/%d traffic", depleted.Up, depleted.Down)
	}
	if !depleted.Enable {
		t.Error("depleted client was not enabled again, so the reset bought it nothing")
	}

	active := loadClient(t, "active@example.com")
	if active.Up != 0 || active.Down != 0 {
		t.Errorf("active client still has %d/%d traffic", active.Up, active.Down)
	}
	if !active.Enable {
		t.Error("active client was disabled by the reset")
	}
}

func TestResetTrafficJobRecordsTheNextBoundary(t *testing.T) {
	newTestDatabase(t)
	seedClients(t)
	settingService := service.SettingService{}

	before := time.Now().Unix()
	newResetJob(t, "@daily").Run()

	next, err := settingService.GetGlobalResetLast()
	if err != nil {
		t.Fatalf("GetGlobalResetLast: %v", err)
	}
	if next <= before {
		t.Fatalf("recorded boundary %d is not in the future", next)
	}
	if next > before+2*24*3600 {
		t.Fatalf("recorded boundary %d is more than two days out for a daily schedule", next)
	}
}

// The job fires on every cron tick, but a reset is only due once the recorded
// boundary has passed; otherwise a restart-heavy panel would reset repeatedly.
func TestResetTrafficJobWaitsForTheBoundary(t *testing.T) {
	newTestDatabase(t)
	seedClients(t)
	settingService := service.SettingService{}

	boundary := time.Now().Add(12 * time.Hour).Unix()
	if err := settingService.SetGlobalResetLast(boundary); err != nil {
		t.Fatalf("SetGlobalResetLast: %v", err)
	}

	newResetJob(t, "@daily").Run()

	active := loadClient(t, "active@example.com")
	if active.Up != 5 || active.Down != 5 {
		t.Fatalf("traffic was reset before the boundary: %d/%d", active.Up, active.Down)
	}
	if depleted := loadClient(t, "depleted@example.com"); depleted.Enable {
		t.Error("a depleted client was enabled before the boundary")
	}
	if got, _ := settingService.GetGlobalResetLast(); got != boundary {
		t.Fatalf("boundary moved to %d, want it left at %d", got, boundary)
	}
}

// Downtime spanning several periods must still cost exactly one reset, not one
// per missed period.
func TestResetTrafficJobResetsOnceAfterDowntime(t *testing.T) {
	newTestDatabase(t)
	seedClients(t)
	settingService := service.SettingService{}

	if err := settingService.SetGlobalResetLast(time.Now().Add(-72 * time.Hour).Unix()); err != nil {
		t.Fatalf("SetGlobalResetLast: %v", err)
	}

	job := newResetJob(t, "@daily")
	job.Run()

	firstBoundary, _ := settingService.GetGlobalResetLast()
	if firstBoundary <= time.Now().Unix() {
		t.Fatalf("boundary %d did not snap forward past the missed periods", firstBoundary)
	}

	// Give the clients traffic again and run once more: nothing should happen.
	db := database.GetDB()
	if err := db.Model(&xray.ClientTraffic{}).Where("email = ?", "active@example.com").
		Updates(map[string]interface{}{"up": 7, "down": 3}).Error; err != nil {
		t.Fatalf("re-seed traffic: %v", err)
	}

	job.Run()

	active := loadClient(t, "active@example.com")
	if active.Up != 7 || active.Down != 3 {
		t.Fatalf("a second run inside the same period reset the traffic: %d/%d", active.Up, active.Down)
	}
}
