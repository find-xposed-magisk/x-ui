package service

import (
	"sync"
	"testing"

	"github.com/alireza0/x-ui/util/tgchat"

	tgbotapi "github.com/go-telegram/bot"
)

// The bot is started and stopped from one goroutine while cron jobs, the login
// handler and the update workers read its state from others. This test drives
// that shape directly; under -race it fails if the state is ever read without
// synchronisation.
func TestBotStateSurvivesConcurrentStartStop(t *testing.T) {
	calls := withFakeBot(t, []tgchat.Chat{{ChatID: 111}, {ChatID: -100222, TopicID: 7}})
	tgbot := Tgbot{}

	// A second bot to swap in, so the state really changes rather than being
	// stored over with the same pointer.
	other, err := tgbotapi.New("other:token", tgbotapi.WithSkipGetMe())
	if err != nil {
		t.Fatalf("create second bot: %v", err)
	}

	const rounds = 40
	var wg sync.WaitGroup

	// The lifecycle goroutine: what Start and Stop do.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			current := state.Load()
			if i%2 == 0 {
				state.Store(&botState{api: other, adminChats: []tgchat.Chat{{ChatID: 999}}})
			} else if current != nil {
				state.Store(current)
			}
			tgbot.Stop()
		}
	}()

	// The readers: a cron job, the login handler, and the panel asking whether
	// the bot is up.
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				tgbot.IsRunning()
				tgbot.SendMsgToTgbotAdmins("report")
				if current := state.Load(); current != nil {
					current.isAdmin(111)
				}
			}
		}()
	}
	wg.Wait()

	// Nothing is asserted about how many messages got through: a send that finds
	// the bot stopped is meant to be dropped. Reaching here without a race
	// report or a panic is the result.
	_ = calls
}

// A reader must cope with the bot being stopped mid-call rather than panicking
// on a nil bot.
func TestBotCallsAreSafeWhileStopped(t *testing.T) {
	previous := state.Load()
	t.Cleanup(func() { state.Store(previous) })
	state.Store(nil)

	tgbot := Tgbot{}
	if tgbot.IsRunning() {
		t.Fatal("IsRunning is true with no state published")
	}
	// None of these may panic.
	tgbot.SendMsgToTgbot(111, "hello")
	tgbot.SendMsgToTgbotAdmins("hello")
	tgbot.SendBackupToAdmins()
	tgbot.Stop()
	tgbot.Stop() // stopping twice must be harmless

	if err := sendFile(111, "irrelevant.db"); err == nil {
		t.Fatal("sendFile should report that the bot is not running")
	}
}

// Stop must release the receiver exactly once, and leave nothing behind that a
// later reader could pick up.
func TestStopClearsTheState(t *testing.T) {
	previous := state.Load()
	t.Cleanup(func() { state.Store(previous) })

	cancelled := 0
	state.Store(&botState{
		api:        nil,
		stop:       func() { cancelled++ },
		adminChats: []tgchat.Chat{{ChatID: 1}},
	})

	tgbot := Tgbot{}
	tgbot.Stop()
	tgbot.Stop()

	if cancelled != 1 {
		t.Fatalf("the receiver was cancelled %d times, want exactly 1", cancelled)
	}
	if state.Load() != nil {
		t.Fatal("state survived Stop")
	}
	if tgbot.IsRunning() {
		t.Fatal("IsRunning is true after Stop")
	}
}
