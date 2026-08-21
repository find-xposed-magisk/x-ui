package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/alireza0/x-ui/util/tgchat"

	tgbotapi "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// capturedCall is one request the bot made to the (fake) Telegram API. The
// library encodes calls as multipart/form-data, so fields and uploaded files
// are captured separately rather than as a raw body.
type capturedCall struct {
	Method string
	Fields map[string]string
	Files  map[string]string // form field name -> "filename:contents"
}

// fakeTelegram stands in for api.telegram.org and records what the bot sends.
// It is the only way to assert the wire format without a real bot token.
func fakeTelegram(t *testing.T) (*httptest.Server, func() []capturedCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []capturedCall

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		call := capturedCall{
			Method: path[len(path)-1],
			Fields: map[string]string{},
			Files:  map[string]string{},
		}

		if err := r.ParseMultipartForm(8 << 20); err == nil && r.MultipartForm != nil {
			for name, values := range r.MultipartForm.Value {
				if len(values) > 0 {
					call.Fields[name] = values[0]
				}
			}
			for name, headers := range r.MultipartForm.File {
				if len(headers) == 0 {
					continue
				}
				file, err := headers[0].Open()
				if err != nil {
					continue
				}
				contents, _ := io.ReadAll(file)
				file.Close()
				call.Files[name] = headers[0].Filename + ":" + string(contents)
			}
		}

		mu.Lock()
		calls = append(calls, call)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}}`)
	}))

	return server, func() []capturedCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedCall(nil), calls...)
	}
}

// withFakeBot publishes a bot state pointing at the fake server for one test and
// restores the previous one afterwards.
func withFakeBot(t *testing.T, chats []tgchat.Chat) func() []capturedCall {
	t.Helper()
	server, calls := fakeTelegram(t)

	previous := state.Load()
	t.Cleanup(func() {
		server.Close()
		state.Store(previous)
	})

	created, err := tgbotapi.New("test:token",
		tgbotapi.WithServerURL(server.URL),
		tgbotapi.WithSkipGetMe(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}
	state.Store(&botState{api: created, adminChats: chats})
	return calls
}

func decodeJSONField(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	decoded := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("field is not JSON: %v\n%s", err, raw)
	}
	return decoded
}

func TestSendMsgToTgbotPlainChat(t *testing.T) {
	calls := withFakeBot(t, []tgchat.Chat{{ChatID: 111}})
	tgbot := Tgbot{}

	tgbot.SendMsgToTgbot(111, "hello")

	made := calls()
	if len(made) != 1 {
		t.Fatalf("made %d calls, want 1", len(made))
	}
	if made[0].Method != "sendMessage" {
		t.Fatalf("method = %q, want sendMessage", made[0].Method)
	}
	if got := made[0].Fields["text"]; got != "hello" {
		t.Errorf("text = %q", got)
	}
	if got := made[0].Fields["parse_mode"]; got != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", got)
	}
	if got := made[0].Fields["chat_id"]; got != "111" {
		t.Errorf("chat_id = %q", got)
	}
	// omitempty must keep the field out entirely for a chat without a topic,
	// because Telegram rejects message_thread_id on a non-forum chat.
	if _, present := made[0].Fields["message_thread_id"]; present {
		t.Errorf("message_thread_id was sent for a chat with no topic: %v", made[0].Fields)
	}
}

func TestSendMsgToTgbotUsesTopic(t *testing.T) {
	calls := withFakeBot(t, []tgchat.Chat{{ChatID: -1001234567890, TopicID: 42}})
	tgbot := Tgbot{}

	tgbot.SendMsgToTgbot(-1001234567890, "into the topic")

	made := calls()
	if len(made) != 1 {
		t.Fatalf("made %d calls, want 1", len(made))
	}
	if got := made[0].Fields["message_thread_id"]; got != "42" {
		t.Fatalf("message_thread_id = %q, want 42 (fields: %v)", got, made[0].Fields)
	}
	if got := made[0].Fields["chat_id"]; got != "-1001234567890" {
		t.Fatalf("chat_id = %q", got)
	}
}

// A user who is not an admin has no configured topic, so their reply must go to
// the plain chat even while an admin group with a topic is configured.
func TestSendMsgToTgbotUnknownChatHasNoTopic(t *testing.T) {
	calls := withFakeBot(t, []tgchat.Chat{{ChatID: -1001234567890, TopicID: 42}})
	tgbot := Tgbot{}

	tgbot.SendMsgToTgbot(999, "to a client")

	if _, present := calls()[0].Fields["message_thread_id"]; present {
		t.Errorf("a non-admin chat was given a topic: %v", calls()[0].Fields)
	}
}

func TestSendMsgToTgbotCarriesKeyboard(t *testing.T) {
	calls := withFakeBot(t, []tgchat.Chat{{ChatID: 111}})
	tgbot := Tgbot{}

	tgbot.SendMsgToTgbot(111, "pick one", models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: "Usage", CallbackData: "get_usage"}},
		},
	})

	raw, present := calls()[0].Fields["reply_markup"]
	if !present {
		t.Fatalf("reply_markup was not sent: %v", calls()[0].Fields)
	}
	markup := decodeJSONField(t, raw)
	rows, ok := markup["inline_keyboard"].([]interface{})
	if !ok || len(rows) != 1 {
		t.Fatalf("inline_keyboard = %v", markup["inline_keyboard"])
	}
}

// Long reports are split so each part stays under Telegram's message limit.
func TestSendMsgToTgbotPagesLongMessages(t *testing.T) {
	calls := withFakeBot(t, []tgchat.Chat{{ChatID: 111}})
	tgbot := Tgbot{}

	block := strings.Repeat("x", 900)
	tgbot.SendMsgToTgbot(111, strings.Join([]string{block, block, block}, "\r\n \r\n"))

	made := calls()
	if len(made) < 2 {
		t.Fatalf("made %d calls, want the message split into several", len(made))
	}
	for i, call := range made {
		if text := call.Fields["text"]; len(text) > 2000 {
			t.Errorf("part %d is %d characters, over the limit", i, len(text))
		}
	}
}

func TestSendMsgToTgbotIgnoresEmptyMessage(t *testing.T) {
	calls := withFakeBot(t, []tgchat.Chat{{ChatID: 111}})
	tgbot := Tgbot{}

	tgbot.SendMsgToTgbot(111, "")

	if made := calls(); len(made) != 0 {
		t.Fatalf("made %d calls for an empty message, want none", len(made))
	}
}

func TestSendFileUsesTopic(t *testing.T) {
	calls := withFakeBot(t, []tgchat.Chat{{ChatID: -100999, TopicID: 7}})

	path := t.TempDir() + "/x-ui.db"
	if err := writeFile(path, "backup-bytes"); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := sendFile(-100999, path); err != nil {
		t.Fatalf("sendFile: %v", err)
	}

	made := calls()
	if len(made) != 1 || made[0].Method != "sendDocument" {
		t.Fatalf("calls = %+v, want one sendDocument", made)
	}
	if got := made[0].Fields["message_thread_id"]; got != "7" {
		t.Errorf("message_thread_id = %q, want 7", got)
	}
	if got := made[0].Files["document"]; got != "x-ui.db:backup-bytes" {
		t.Errorf("uploaded document = %q, want the file's real name and contents", got)
	}
}

func TestSendFileReportsMissingFile(t *testing.T) {
	withFakeBot(t, nil)
	if err := sendFile(111, t.TempDir()+"/does-not-exist.db"); err == nil {
		t.Fatal("expected an error for a file that cannot be opened")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
