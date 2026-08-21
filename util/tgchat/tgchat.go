// Package tgchat models the Telegram destinations the panel reports to.
package tgchat

import (
	"strconv"
	"strings"

	"github.com/alireza0/x-ui/util/common"
)

// Chat is one destination the bot reports to. Telegram supergroups can be
// split into topics, and an admin who runs one group for several servers wants
// each panel writing into its own topic rather than the shared history, so a
// chat id may carry a topic id alongside it.
type Chat struct {
	ChatID  int64
	TopicID int
}

// Parse reads the "Admin Chat ID" setting: a comma-separated list
// where each entry is a chat id, optionally suffixed with ":<topic id>".
// Chat ids are negative for groups, so the colon is unambiguous.
func Parse(raw string) ([]Chat, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	chats := make([]Chat, 0)
	for _, entry := range strings.Split(trimmed, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		idPart, topicPart, hasTopic := strings.Cut(entry, ":")
		chatID, err := strconv.ParseInt(strings.TrimSpace(idPart), 10, 64)
		if err != nil {
			return nil, common.NewErrorf("chat id <%v> is not a number", strings.TrimSpace(idPart))
		}

		chat := Chat{ChatID: chatID}
		if hasTopic {
			topicID, err := strconv.Atoi(strings.TrimSpace(topicPart))
			if err != nil {
				return nil, common.NewErrorf("topic id <%v> of chat <%v> is not a number", strings.TrimSpace(topicPart), chatID)
			}
			if topicID <= 0 {
				return nil, common.NewErrorf("topic id <%v> of chat <%v> must be positive", topicID, chatID)
			}
			chat.TopicID = topicID
		}
		chats = append(chats, chat)
	}
	return chats, nil
}

// TopicOf returns the topic a message to chatID belongs in, or 0 for the
// chat's main history.
func TopicOf(chats []Chat, chatID int64) int {
	for _, chat := range chats {
		if chat.ChatID == chatID {
			return chat.TopicID
		}
	}
	return 0
}
