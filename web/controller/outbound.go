package controller

import (
	"encoding/json"
	"strconv"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/web/service"
	"github.com/gin-gonic/gin"
)

type OutboundController struct {
	outboundService service.OutboundService
	inboundService  service.InboundService
	xrayService     service.XrayService
}

func NewOutboundController(g *gin.RouterGroup) *OutboundController {
	a := &OutboundController{}
	a.initRouter(g)
	return a
}

func (a *OutboundController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/outbound")

	g.POST("/list", a.getOutbounds)
	g.POST("/add", a.addOutbound)
	g.POST("/del/:id", a.delOutbound)
	g.POST("/update/:id", a.updateOutbound)
	g.POST("/setFirst/:id", a.setFirstOutbound)
	g.POST("/:id/resetTraffic", a.resetTraffic)
	g.POST("/resetAllTraffics", a.resetAllTraffics)
	g.POST("/onlines", a.onlines)
	g.POST("/test", a.test)
	g.POST("/reverseTags", a.getClientReverseTags)
}

func (a *OutboundController) getOutbounds(c *gin.Context) {
	outbounds, err := a.outboundService.GetAllOutbounds()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.toasts.obtain"), err)
		return
	}
	jsonObj(c, outbounds, nil)
}

func (a *OutboundController) addOutbound(c *gin.Context) {
	outbound := &model.Outbound{}
	err := c.ShouldBind(outbound)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.create"), err)
		return
	}
	outbound, needRestart, err := a.outboundService.AddOutbound(outbound)
	jsonMsgObj(c, I18nWeb(c, "pages.outbounds.create"), outbound, err)
	if err == nil && needRestart {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *OutboundController) delOutbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.delete"), err)
		return
	}
	needRestart, err := a.outboundService.DelOutbound(id)
	jsonMsg(c, I18nWeb(c, "pages.outbounds.delete"), err)
	if err == nil && needRestart {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *OutboundController) updateOutbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.update"), err)
		return
	}
	outbound := &model.Outbound{Id: id}
	err = c.ShouldBind(outbound)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.update"), err)
		return
	}
	outbound, needRestart, err := a.outboundService.UpdateOutbound(outbound)
	jsonMsgObj(c, I18nWeb(c, "pages.outbounds.update"), outbound, err)
	if err == nil && needRestart {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *OutboundController) setFirstOutbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.update"), err)
		return
	}
	err = a.outboundService.SetFirstOutbound(id)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
	jsonMsg(c, I18nWeb(c, "pages.outbounds.update"), err)
}

func (a *OutboundController) resetTraffic(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.resetTraffic"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.outbounds.resetTraffic"), a.outboundService.ResetTraffic(id))
}

func (a *OutboundController) resetAllTraffics(c *gin.Context) {
	jsonMsg(c, I18nWeb(c, "pages.outbounds.resetAllTraffic"), a.outboundService.ResetAllTraffics())
}

func (a *OutboundController) onlines(c *gin.Context) {
	jsonObj(c, a.outboundService.GetOnlineOutbounds(), nil)
}

func (a *OutboundController) getClientReverseTags(c *gin.Context) {
	tags, err := a.inboundService.GetClientReverseTags()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.toasts.obtain"), err)
		return
	}
	var arr []string
	json.Unmarshal([]byte(tags), &arr)
	jsonObj(c, arr, nil)
}

func (a *OutboundController) test(c *gin.Context) {
	id, err := strconv.Atoi(c.PostForm("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.outbounds.test"), err)
		return
	}
	result, err := a.outboundService.TestOutbound(id)
	jsonObj(c, result, err)
}
