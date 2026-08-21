package tgchat

import (
	"strings"
	"testing"
)

func TestParseSingleChat(t *testing.T) {
	chats, err := Parse("123456789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("got %d chats, want 1", len(chats))
	}
	if chats[0].ChatID != 123456789 || chats[0].TopicID != 0 {
		t.Fatalf("chat = %+v, want id 123456789 with no topic", chats[0])
	}
}

// Group chat ids are negative, which is exactly why the topic separator is a
// colon rather than a minus or a dash.
func TestParseSupergroupWithTopic(t *testing.T) {
	chats, err := Parse("-1001234567890:42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("got %d chats, want 1", len(chats))
	}
	if chats[0].ChatID != -1001234567890 {
		t.Fatalf("chat id = %d, want -1001234567890", chats[0].ChatID)
	}
	if chats[0].TopicID != 42 {
		t.Fatalf("topic id = %d, want 42", chats[0].TopicID)
	}
}

func TestParseMixedList(t *testing.T) {
	chats, err := Parse(" 111 , -1001234567890:42 ,222 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Chat{
		{ChatID: 111},
		{ChatID: -1001234567890, TopicID: 42},
		{ChatID: 222},
	}
	if len(chats) != len(want) {
		t.Fatalf("got %v, want %v", chats, want)
	}
	for i := range want {
		if chats[i] != want[i] {
			t.Fatalf("chat %d = %+v, want %+v", i, chats[i], want[i])
		}
	}
}

func TestParseEmptyMeansNoChats(t *testing.T) {
	for _, raw := range []string{"", "   ", ",", " , "} {
		chats, err := Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", raw, err)
		}
		if len(chats) != 0 {
			t.Fatalf("%q parsed to %v, want nothing", raw, chats)
		}
	}
}

func TestParseRejectsBadValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"not a number", "abc"},
		{"username instead of id", "@someone"},
		{"topic is not a number", "123:abc"},
		{"topic is zero", "123:0"},
		{"topic is negative", "123:-1"},
		{"one bad entry in a list", "111,abc,222"},
		{"float id", "12.5"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.raw); err == nil {
				t.Fatalf("expected %q to be rejected", test.raw)
			}
		})
	}
}

// The message is shown in the settings page, so it must point at the offending
// entry rather than just say the list is invalid.
func TestParseErrorNamesTheBadEntry(t *testing.T) {
	_, err := Parse("111,nope,222")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error %q does not name the rejected entry", err.Error())
	}
}

func TestTopicOf(t *testing.T) {
	chats := []Chat{
		{ChatID: 111},
		{ChatID: -1001234567890, TopicID: 42},
	}
	if got := TopicOf(chats, -1001234567890); got != 42 {
		t.Fatalf("TopicOf = %d, want 42", got)
	}
	if got := TopicOf(chats, 111); got != 0 {
		t.Fatalf("TopicOf = %d, want 0 for a chat with no topic", got)
	}
	// A callback can arrive from a user who is not in the admin list at all.
	if got := TopicOf(chats, 999); got != 0 {
		t.Fatalf("TopicOf = %d, want 0 for an unknown chat", got)
	}
	if got := TopicOf(nil, 111); got != 0 {
		t.Fatalf("TopicOf = %d, want 0 with no chats configured", got)
	}
}
