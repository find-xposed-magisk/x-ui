package service

import (
	"strings"

	"github.com/go-telegram/bot/models"
)

// The bot library exposes raw updates and leaves command parsing to the caller.
// These helpers reproduce the semantics the panel relied on before, so the same
// message keeps reaching the same branch of answerCommand.

// isCommand reports whether the message opens with a bot command, which is the
// only position Telegram treats as a command.
func isCommand(message *models.Message) bool {
	if message == nil || len(message.Entities) == 0 {
		return false
	}
	entity := message.Entities[0]
	return entity.Offset == 0 && entity.Type == models.MessageEntityTypeBotCommand
}

// commandOf returns the command without its leading slash and without the
// "@botname" suffix Telegram appends in groups, or "" for a plain message.
func commandOf(message *models.Message) string {
	if !isCommand(message) {
		return ""
	}
	command := entityText(message, message.Entities[0])
	command = strings.TrimPrefix(command, "/")
	if at := strings.Index(command, "@"); at != -1 {
		command = command[:at]
	}
	return command
}

// commandArgumentsOf returns everything after the command name, dropping the
// single separator character between them.
func commandArgumentsOf(message *models.Message) string {
	if !isCommand(message) {
		return ""
	}
	runes := []rune(message.Text)
	end := message.Entities[0].Length
	if end < 0 || end >= len(runes) {
		// Either the command makes up the whole message, or the entity is
		// malformed; both mean there is nothing to hand on as an argument.
		return ""
	}
	return string(runes[end+1:])
}

// entityText slices the message by the entity's offset and length. Telegram
// counts both in UTF-16 code units while Go indexes bytes, but a command is
// always ASCII and always at offset 0, so counting runes is enough here and
// avoids mangling a multi-byte argument that follows.
func entityText(message *models.Message, entity models.MessageEntity) string {
	runes := []rune(message.Text)
	start := entity.Offset
	end := start + entity.Length
	// Telegram never sends a negative offset or length, but the fields are plain
	// ints and this parser runs on whatever arrives from the network.
	if start < 0 || start > len(runes) || end < start {
		return ""
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end])
}
