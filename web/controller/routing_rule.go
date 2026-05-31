package controller

import (
	"encoding/json"
	"strconv"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/web/service"
	"github.com/gin-gonic/gin"
)

type RoutingRuleController struct {
	routingRuleService service.RoutingRuleService
	inboundService     service.InboundService
	outboundService    service.OutboundService
	xrayService        service.XrayService
}

func NewRoutingRuleController(g *gin.RouterGroup) *RoutingRuleController {
	a := &RoutingRuleController{}
	a.initRouter(g)
	return a
}

func (a *RoutingRuleController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/routing")

	g.POST("/list", a.getRules)
	g.POST("/meta", a.getMeta)
	g.POST("/meta/update", a.updateMeta)
	g.POST("/add", a.addRule)
	g.POST("/del/:id", a.delRule)
	g.POST("/update/:id", a.updateRule)
	g.POST("/reorder", a.reorderRules)
	g.POST("/syncBasic", a.syncBasic)
	g.POST("/getBasic", a.getBasic)
	g.POST("/replaceBalancerTag", a.replaceBalancerTag)
	g.POST("/refs", a.getRefs)
}

func jsonStringArray(s string) []string {
	var arr []string
	_ = json.Unmarshal([]byte(s), &arr)
	return arr
}

func (a *RoutingRuleController) getRefs(c *gin.Context) {
	inboundTags, _ := a.inboundService.GetInboundTags()
	clientReverseTags, _ := a.inboundService.GetClientReverseTags()
	outboundTags, _ := a.outboundService.GetOutboundTags()
	outboundReverseTags, _ := a.outboundService.GetOutboundReverseTags()
	meta, _ := a.routingRuleService.GetRoutingMeta()
	jsonObj(c, map[string]any{
		"inboundTags":         jsonStringArray(inboundTags),
		"clientReverseTags":   jsonStringArray(clientReverseTags),
		"outboundTags":        jsonStringArray(outboundTags),
		"outboundReverseTags": jsonStringArray(outboundReverseTags),
		"routingMeta":         meta,
	}, nil)
}

func (a *RoutingRuleController) getRules(c *gin.Context) {
	rules, err := a.routingRuleService.GetAllRules()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.toasts.obtain"), err)
		return
	}
	jsonObj(c, rules, nil)
}

func (a *RoutingRuleController) getMeta(c *gin.Context) {
	meta, err := a.routingRuleService.GetRoutingMeta()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.toasts.obtain"), err)
		return
	}
	jsonObj(c, meta, nil)
}

type routingMetaForm struct {
	DomainStrategy string `json:"domainStrategy" form:"domainStrategy"`
}

func (a *RoutingRuleController) updateMeta(c *gin.Context) {
	form := routingMetaForm{}
	if err := c.ShouldBind(&form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.updateMeta"), err)
		return
	}
	err := a.routingRuleService.SaveRoutingMeta(form.DomainStrategy)
	jsonMsg(c, I18nWeb(c, "pages.routingRules.updateMeta"), err)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *RoutingRuleController) addRule(c *gin.Context) {
	rule := &model.RoutingRule{}
	if err := c.ShouldBind(rule); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.create"), err)
		return
	}
	rule, needRestart, err := a.routingRuleService.AddRule(rule)
	jsonMsgObj(c, I18nWeb(c, "pages.routingRules.create"), rule, err)
	if err == nil && needRestart {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *RoutingRuleController) delRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.delete"), err)
		return
	}
	needRestart, err := a.routingRuleService.DelRule(id)
	jsonMsg(c, I18nWeb(c, "pages.routingRules.delete"), err)
	if err == nil && needRestart {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *RoutingRuleController) updateRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.update"), err)
		return
	}
	rule := &model.RoutingRule{Id: id}
	if err := c.ShouldBind(rule); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.update"), err)
		return
	}
	rule, needRestart, err := a.routingRuleService.UpdateRule(rule)
	jsonMsgObj(c, I18nWeb(c, "pages.routingRules.update"), rule, err)
	if err == nil && needRestart {
		a.xrayService.SetToNeedRestart()
	}
}

type reorderRulesForm struct {
	Ids []int `json:"ids" form:"ids"`
}

func (a *RoutingRuleController) reorderRules(c *gin.Context) {
	form := reorderRulesForm{}
	if err := c.ShouldBind(&form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.update"), err)
		return
	}
	err := a.routingRuleService.ReorderRules(form.Ids)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
	jsonMsg(c, I18nWeb(c, "pages.routingRules.update"), err)
}

type syncBasicForm struct {
	OutboundTag string   `json:"outboundTag" form:"outboundTag"`
	Property    string   `json:"property" form:"property"`
	Data        []string `json:"data" form:"data"`
}

func (a *RoutingRuleController) getBasic(c *gin.Context) {
	form := syncBasicForm{}
	if err := c.ShouldBind(&form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.toasts.obtain"), err)
		return
	}
	data, err := a.routingRuleService.GetBasicProperty(form.OutboundTag, form.Property)
	jsonObj(c, data, err)
}

type replaceBalancerForm struct {
	OldTag string `json:"oldTag" form:"oldTag"`
	NewTag string `json:"newTag" form:"newTag"`
}

func (a *RoutingRuleController) replaceBalancerTag(c *gin.Context) {
	form := replaceBalancerForm{}
	if err := c.ShouldBind(&form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.update"), err)
		return
	}
	err := a.routingRuleService.ReplaceBalancerTag(form.OldTag, form.NewTag)
	jsonMsg(c, I18nWeb(c, "pages.routingRules.update"), err)
	if err == nil {
		a.xrayService.SetToNeedRestart()
	}
}

func (a *RoutingRuleController) syncBasic(c *gin.Context) {
	form := syncBasicForm{}
	if err := c.ShouldBind(&form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routingRules.update"), err)
		return
	}
	err := a.routingRuleService.SyncBasicProperty(form.OutboundTag, form.Property, form.Data)
	jsonMsg(c, I18nWeb(c, "pages.routingRules.update"), err)
}
