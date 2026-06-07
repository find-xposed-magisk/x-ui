package job

import (
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/web/service"
)

type IpLimitJob struct {
	xrayService service.XrayService
}

func NewIpLimitJob() *IpLimitJob {
	return new(IpLimitJob)
}

func (j *IpLimitJob) Run() {
	if err := j.xrayService.RefreshOnlineUsersCache(); err != nil {
		logger.Warning("refresh online users failed:", err)
		return
	}

	service.ProcessIpLimitCron(j.xrayService.GetOnlineUsers())
}
