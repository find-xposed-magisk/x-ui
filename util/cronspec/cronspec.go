// Package cronspec parses the cron expressions the panel accepts from an admin.
package cronspec

import (
	"strings"

	"github.com/alireza0/x-ui/util/common"

	"github.com/robfig/cron/v3"
)

// Parser accepts a standard five-field cron expression, a six-field one with a
// leading seconds column, and the @descriptors. The panel's own scheduler is
// built with cron.WithSeconds(), whose parser rejects five-field expressions,
// so admin-supplied schedules are parsed here instead and handed to the
// scheduler as an already-parsed cron.Schedule.
var Parser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// Off is the value an admin can type to disable a schedule, alongside leaving
// the field empty.
const Off = "off"

// Parse returns the schedule for spec, or nil when the schedule is disabled.
func Parse(spec string) (cron.Schedule, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" || strings.EqualFold(trimmed, Off) {
		return nil, nil
	}
	schedule, err := Parser.Parse(trimmed)
	if err != nil {
		return nil, common.NewErrorf("cron schedule <%v> is not valid: %v", trimmed, err)
	}
	return schedule, nil
}
