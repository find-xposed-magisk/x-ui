package job

import (
	"time"

	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/web/service"

	"github.com/robfig/cron/v3"
)

// ResetTrafficJob zeroes every client's traffic on a schedule the admin sets,
// so an "unlimited" plan can still be sold with a fair per-period budget.
type ResetTrafficJob struct {
	inboundService service.InboundService
	settingService service.SettingService
	xrayService    service.XrayService
	schedule       cron.Schedule
}

func NewResetTrafficJob(schedule cron.Schedule) *ResetTrafficJob {
	return &ResetTrafficJob{schedule: schedule}
}

func (j *ResetTrafficJob) Run() {
	loc, err := j.settingService.GetTimeLocation()
	if err != nil {
		logger.Warning("reset traffic: get time location failed:", err)
		return
	}
	now := time.Now().In(loc)

	next, err := j.settingService.GetGlobalResetLast()
	if err != nil {
		logger.Warning("reset traffic: get last reset time failed:", err)
		return
	}
	// The panel may have been down across several boundaries, or the schedule
	// may have been edited; either way one reset is enough, so wait until the
	// boundary recorded last time is actually behind us.
	if next > now.Unix() {
		return
	}

	if err := j.inboundService.ResetAllClientTraffics(-1); err != nil {
		logger.Warning("reset traffic: reset all clients failed:", err)
		return
	}

	if err := j.settingService.SetGlobalResetLast(j.schedule.Next(now).Unix()); err != nil {
		logger.Warning("reset traffic: set last reset time failed:", err)
	}

	// Clients that ran out are enabled again, and Xray only learns that from a
	// fresh config.
	j.xrayService.SetToNeedRestart()
	logger.Info("reset traffic: all client traffics reset, next reset at ", j.schedule.Next(now).In(loc).Format(time.RFC3339))
}
