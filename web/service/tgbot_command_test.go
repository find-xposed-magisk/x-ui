package service

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

// commandMessage builds the update Telegram sends for a command: the entity
// spans the "/name" token at the start of the text.
func commandMessage(text string, commandLen int) *models.Message {
	return &models.Message{
		Text: text,
		Entities: []models.MessageEntity{
			{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: commandLen},
		},
	}
}

func TestCommandParsing(t *testing.T) {
	tests := []struct {
		name       string
		message    *models.Message
		command    string
		args       string
		isACommand bool
	}{
		{
			name:       "bare command",
			message:    commandMessage("/start", 6),
			command:    "start",
			args:       "",
			isACommand: true,
		},
		{
			// Telegram appends the bot name when a command is used in a group.
			name:       "command addressed to the bot",
			message:    commandMessage("/start@my_panel_bot", 19),
			command:    "start",
			args:       "",
			isACommand: true,
		},
		{
			name:       "command with an argument",
			message:    commandMessage("/usage 3f1a-uuid", 6),
			command:    "usage",
			args:       "3f1a-uuid",
			isACommand: true,
		},
		{
			name:       "addressed command with an argument",
			message:    commandMessage("/usage@my_panel_bot 3f1a-uuid", 19),
			command:    "usage",
			args:       "3f1a-uuid",
			isACommand: true,
		},
		{
			name:       "argument keeps its inner spaces",
			message:    commandMessage("/inbound my inbound name", 8),
			command:    "inbound",
			args:       "my inbound name",
			isACommand: true,
		},
		{
			// Emails and remarks are routinely non-ASCII; slicing by bytes here
			// would cut a multi-byte rune in half.
			name:       "multi-byte argument",
			message:    commandMessage("/inbound سرور اصلی", 8),
			command:    "inbound",
			args:       "سرور اصلی",
			isACommand: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isCommand(test.message); got != test.isACommand {
				t.Fatalf("isCommand = %v, want %v", got, test.isACommand)
			}
			if got := commandOf(test.message); got != test.command {
				t.Errorf("commandOf = %q, want %q", got, test.command)
			}
			if got := commandArgumentsOf(test.message); got != test.args {
				t.Errorf("commandArgumentsOf = %q, want %q", got, test.args)
			}
		})
	}
}

func TestNotACommand(t *testing.T) {
	tests := []struct {
		name    string
		message *models.Message
	}{
		{"nil message", nil},
		{"plain text", &models.Message{Text: "hello"}},
		{"no entities", &models.Message{Text: "/start"}},
		{
			// "look at /start" mentions a command but does not invoke one.
			name: "command not at the start",
			message: &models.Message{
				Text: "look at /start",
				Entities: []models.MessageEntity{
					{Type: models.MessageEntityTypeBotCommand, Offset: 8, Length: 6},
				},
			},
		},
		{
			name: "a different entity type",
			message: &models.Message{
				Text: "@someone hi",
				Entities: []models.MessageEntity{
					{Type: models.MessageEntityTypeMention, Offset: 0, Length: 8},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if isCommand(test.message) {
				t.Error("isCommand = true, want false")
			}
			if got := commandOf(test.message); got != "" {
				t.Errorf("commandOf = %q, want empty", got)
			}
			if got := commandArgumentsOf(test.message); got != "" {
				t.Errorf("commandArgumentsOf = %q, want empty", got)
			}
		})
	}
}

// A malformed entity must not panic the update handler, since it would take the
// whole receiver goroutine down with it.
func TestCommandParsingSurvivesBadEntities(t *testing.T) {
	tests := []struct {
		name    string
		message *models.Message
	}{
		{"length past the end", commandMessage("/hi", 99)},
		{"zero length", commandMessage("/hi", 0)},
		{"empty text", commandMessage("", 5)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			commandOf(test.message)
			commandArgumentsOf(test.message)
		})
	}
}

// The dispatcher in answerCommand switches on these exact strings; a parser
// change that renamed any of them would silently disable that command.
func TestKnownCommandsStillParse(t *testing.T) {
	for _, name := range []string{"help", "start", "status", "id", "usage", "inbound"} {
		message := commandMessage("/"+name, len(name)+1)
		if got := commandOf(message); got != name {
			t.Errorf("commandOf(/%s) = %q", name, got)
		}
	}
}
