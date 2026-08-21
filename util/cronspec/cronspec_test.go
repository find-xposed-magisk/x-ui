package cronspec

import (
	"strings"
	"testing"
	"time"
)

func TestParseAcceptsSupportedForms(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{"daily descriptor", "@daily"},
		{"weekly descriptor", "@weekly"},
		{"monthly descriptor", "@monthly"},
		{"midnight descriptor", "@midnight"},
		{"every duration", "@every 12h"},
		// The panel's own scheduler is built with cron.WithSeconds(), whose
		// parser rejects this five-field form; parsing it here is the whole
		// reason this package exists.
		{"standard five-field", "0 0 * * *"},
		{"five-field with a list", "30 3,15 * * 1-5"},
		{"six-field with seconds", "0 0 0 * * *"},
		{"surrounding whitespace", "  @daily  "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedule, err := Parse(test.spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if schedule == nil {
				t.Fatal("a valid schedule parsed to nil, which means disabled")
			}
			if next := schedule.Next(time.Now()); next.Before(time.Now()) {
				t.Fatalf("next occurrence %v is in the past", next)
			}
		})
	}
}

func TestParseTreatsBlankAndOffAsDisabled(t *testing.T) {
	for _, spec := range []string{"", "   ", "off", "OFF", " Off "} {
		schedule, err := Parse(spec)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", spec, err)
		}
		if schedule != nil {
			t.Fatalf("%q should disable the schedule, got %v", spec, schedule)
		}
	}
}

func TestParseRejectsBadSpecs(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{"not a schedule", "every day"},
		{"unknown descriptor", "@yearly-ish"},
		{"too few fields", "0 0"},
		{"field out of range", "0 99 * * *"},
		{"bad duration", "@every banana"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.spec); err == nil {
				t.Fatalf("expected %q to be rejected", test.spec)
			}
		})
	}
}

// The message reaches the admin in the settings page, so it has to quote what
// they typed rather than just say the schedule is invalid.
func TestParseErrorQuotesTheSpec(t *testing.T) {
	_, err := Parse("every day")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "every day") {
		t.Fatalf("error %q does not quote the rejected spec", err.Error())
	}
}

// @daily must land on local midnight, since that is what an admin selling a
// daily budget expects the day to roll over at.
func TestDailyLandsOnLocalMidnight(t *testing.T) {
	schedule, err := Parse("@daily")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tehran, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Skipf("time zone database unavailable: %v", err)
	}

	now := time.Date(2026, 8, 21, 14, 30, 0, 0, tehran)
	next := schedule.Next(now)
	if next.Hour() != 0 || next.Minute() != 0 {
		t.Fatalf("next daily reset at %v, want midnight", next)
	}
	if next.Day() != 22 {
		t.Fatalf("next daily reset on day %d, want the following day", next.Day())
	}
}
