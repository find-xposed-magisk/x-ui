package service

import (
	"path/filepath"
	"testing"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/web/entity"
)

// newSettingService points the package-level database at a throwaway file, so
// the settings round-trip is exercised against real SQLite rather than a stub.
func newSettingService(t *testing.T) *SettingService {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	return &SettingService{}
}

// baselineSetting is the smallest settings object UpdateAllSetting accepts.
func baselineSetting() *entity.AllSetting {
	return &entity.AllSetting{
		WebPort:      2053,
		SubPort:      2096,
		WebBasePath:  "/",
		SubPath:      "/sub/",
		SubJsonPath:  "/json/",
		TimeLocation: "Asia/Tehran",
	}
}

// A panel upgraded from an older version has no row for a newly added setting,
// so the getter has to fall back to the default instead of erroring out and
// stopping the bot from starting.
func TestNewTelegramSettingsDefaultOnUpgradedDatabases(t *testing.T) {
	service := newSettingService(t)

	proxy, err := service.GetTgBotProxy()
	if err != nil {
		t.Fatalf("GetTgBotProxy on a database without the row: %v", err)
	}
	if proxy != "" {
		t.Fatalf("proxy defaulted to %q, want a direct connection", proxy)
	}

	notifyOnly, err := service.GetTgBotNotifyOnly()
	if err != nil {
		t.Fatalf("GetTgBotNotifyOnly on a database without the row: %v", err)
	}
	if notifyOnly {
		t.Fatal("notify-only defaulted to true, which would silence an existing bot")
	}
}

func TestTgBotProxyRoundTrip(t *testing.T) {
	service := newSettingService(t)

	if err := service.SetTgBotProxy("socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("SetTgBotProxy: %v", err)
	}
	got, err := service.GetTgBotProxy()
	if err != nil {
		t.Fatalf("GetTgBotProxy: %v", err)
	}
	if got != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy = %q, want the value just saved", got)
	}

	// Clearing it must be possible, or a misconfigured proxy locks the bot out.
	if err := service.SetTgBotProxy(""); err != nil {
		t.Fatalf("clear TgBotProxy: %v", err)
	}
	if got, _ := service.GetTgBotProxy(); got != "" {
		t.Fatalf("proxy = %q after clearing, want empty", got)
	}
}

// The reset job waits on the boundary it stored; keeping that boundary across a
// schedule change would leave a newly chosen, more frequent schedule inert until
// the old one's boundary finally passed.
func TestChangingGlobalResetClearsTheBoundary(t *testing.T) {
	service := newSettingService(t)

	if err := service.SetGlobalReset("@weekly"); err != nil {
		t.Fatalf("SetGlobalReset: %v", err)
	}
	if err := service.SetGlobalResetLast(4102444800); err != nil { // far in the future
		t.Fatalf("SetGlobalResetLast: %v", err)
	}

	setting := baselineSetting()
	setting.GlobalReset = "@daily"
	if err := service.UpdateAllSetting(setting); err != nil {
		t.Fatalf("UpdateAllSetting: %v", err)
	}

	if got, _ := service.GetGlobalReset(); got != "@daily" {
		t.Fatalf("schedule = %q, want @daily", got)
	}
	if got, _ := service.GetGlobalResetLast(); got != 0 {
		t.Fatalf("boundary = %d, want it cleared so the new schedule applies", got)
	}
}

// An unchanged schedule must keep its boundary, or every unrelated settings save
// would grant an extra reset.
func TestSavingSettingsKeepsAnUnchangedResetBoundary(t *testing.T) {
	service := newSettingService(t)

	if err := service.SetGlobalReset("@daily"); err != nil {
		t.Fatalf("SetGlobalReset: %v", err)
	}
	if err := service.SetGlobalResetLast(4102444800); err != nil {
		t.Fatalf("SetGlobalResetLast: %v", err)
	}

	setting := baselineSetting()
	setting.GlobalReset = "@daily"
	setting.PageSize = 25 // an unrelated change
	if err := service.UpdateAllSetting(setting); err != nil {
		t.Fatalf("UpdateAllSetting: %v", err)
	}

	if got, _ := service.GetGlobalResetLast(); got != 4102444800 {
		t.Fatalf("boundary = %d, want it untouched", got)
	}
}

func TestUpdateAllSettingRejectsBadResetSchedule(t *testing.T) {
	service := newSettingService(t)

	setting := baselineSetting()
	setting.GlobalReset = "every day"
	if err := service.UpdateAllSetting(setting); err == nil {
		t.Fatal("an invalid cron schedule was accepted")
	}
}

// UpdateAllSetting is what the settings page calls; it must persist the new
// fields and refuse an invalid one without writing anything.
func TestUpdateAllSettingPersistsTelegramFields(t *testing.T) {
	service := newSettingService(t)

	setting := baselineSetting()
	setting.TgBotProxy = "socks5://127.0.0.1:1080"
	setting.TgBotChatId = "-1001234567890:42"
	setting.TgBotNotifyOnly = true
	if err := service.UpdateAllSetting(setting); err != nil {
		t.Fatalf("UpdateAllSetting: %v", err)
	}

	if got, _ := service.GetTgBotProxy(); got != "socks5://127.0.0.1:1080" {
		t.Errorf("proxy = %q", got)
	}
	if got, _ := service.GetTgBotChatId(); got != "-1001234567890:42" {
		t.Errorf("chat id = %q", got)
	}
	if got, _ := service.GetTgBotNotifyOnly(); !got {
		t.Error("notify-only was not persisted")
	}

	bad := *setting
	bad.TgBotProxy = "ftp://127.0.0.1:21"
	if err := service.UpdateAllSetting(&bad); err == nil {
		t.Fatal("an invalid proxy was accepted")
	}
	if got, _ := service.GetTgBotProxy(); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy = %q, want the previous value kept after a rejected save", got)
	}
}
