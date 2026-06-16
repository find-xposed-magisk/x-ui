package controller

import (
	"github.com/alireza0/x-ui/web/service"

	"github.com/gin-gonic/gin"
)

type APIController struct {
	BaseController
	inboundController     *InboundController
	outboundController    *OutboundController
	routingRuleController *RoutingRuleController
	serverController      *ServerController
	Tgbot                 service.Tgbot
}

func NewAPIController(g *gin.RouterGroup, s *ServerController) *APIController {
	a := &APIController{
		serverController: s,
	}
	a.initRouter(g)
	return a
}

func (a *APIController) initRouter(g *gin.RouterGroup) {
	api := g.Group("/xui/API")
	api.Use(a.checkLogin)

	a.inboundApi(api)
	a.outboundApi(api)
	a.routingApi(api)
	a.serverApi(api)
}

func (a *APIController) inboundApi(api *gin.RouterGroup) {
	inboundsApi := api.Group("/inbounds")

	a.inboundController = &InboundController{}

	inboundRoutes := []struct {
		Method  string
		Path    string
		Handler gin.HandlerFunc
	}{
		{"GET", "/", a.inboundController.getInbounds},
		{"GET", "/get/:id", a.inboundController.getInbound},
		{"GET", "/getClientTraffics/:email", a.inboundController.getClientTraffics},
		{"GET", "/getClientTrafficsById/:id", a.inboundController.getClientTrafficsById},
		{"POST", "/add", a.inboundController.addInbound},
		{"POST", "/del/:id", a.inboundController.delInbound},
		{"POST", "/update/:id", a.inboundController.updateInbound},
		{"POST", "/addClient", a.inboundController.addInboundClient},
		{"POST", "/:id/delClient/:clientId", a.inboundController.delInboundClient},
		{"POST", "/updateClient/:clientId", a.inboundController.updateInboundClient},
		{"POST", "/:id/resetClientTraffic/:email", a.inboundController.resetClientTraffic},
		{"POST", "/resetAllTraffics", a.inboundController.resetAllTraffics},
		{"POST", "/resetAllClientTraffics/:id", a.inboundController.resetAllClientTraffics},
		{"POST", "/delDepletedClients/:id", a.inboundController.delDepletedClients},
		{"POST", "/import", a.inboundController.importInbound},
		{"POST", "/onlines", a.inboundController.onlines},
	}

	for _, route := range inboundRoutes {
		inboundsApi.Handle(route.Method, route.Path, route.Handler)
	}
}

func (a *APIController) outboundApi(api *gin.RouterGroup) {
	outboundsApi := api.Group("/outbounds")

	a.outboundController = &OutboundController{}

	outboundRoutes := []struct {
		Method  string
		Path    string
		Handler gin.HandlerFunc
	}{
		{"GET", "/", a.outboundController.getOutbounds},
		{"POST", "/add", a.outboundController.addOutbound},
		{"POST", "/del/:id", a.outboundController.delOutbound},
		{"POST", "/update/:id", a.outboundController.updateOutbound},
		{"POST", "/setFirst/:id", a.outboundController.setFirstOutbound},
		{"POST", "/:id/resetTraffic", a.outboundController.resetTraffic},
		{"POST", "/resetAllTraffics", a.outboundController.resetAllTraffics},
		{"POST", "/onlines", a.outboundController.onlines},
		{"POST", "/test", a.outboundController.test},
	}

	for _, route := range outboundRoutes {
		outboundsApi.Handle(route.Method, route.Path, route.Handler)
	}
}

func (a *APIController) routingApi(api *gin.RouterGroup) {
	routingApi := api.Group("/routing")

	a.routingRuleController = &RoutingRuleController{}

	routingRoutes := []struct {
		Method  string
		Path    string
		Handler gin.HandlerFunc
	}{
		{"GET", "/", a.routingRuleController.getRules},
		{"GET", "/refs", a.routingRuleController.getRefs},
		{"POST", "/save", a.routingRuleController.saveRules},
		{"POST", "/replaceBalancerTag", a.routingRuleController.replaceBalancerTag},
	}

	for _, route := range routingRoutes {
		routingApi.Handle(route.Method, route.Path, route.Handler)
	}
}

func (a *APIController) serverApi(api *gin.RouterGroup) {
	serverApi := api.Group("/server")

	serverRoutes := []struct {
		Method  string
		Path    string
		Handler gin.HandlerFunc
	}{
		{"GET", "/status", a.serverController.status},
		{"GET", "/getDb", a.serverController.getDb},
		{"GET", "/createbackup", a.createBackup},
		{"GET", "/getConfigJson", a.serverController.getConfigJson},
		{"GET", "/getXrayVersion", a.serverController.getXrayVersion},
		{"GET", "/getNewVlessEnc", a.serverController.getNewVlessEnc},
		{"GET", "/getNewX25519Cert", a.serverController.getNewX25519Cert},
		{"GET", "/getNewmldsa65", a.serverController.getNewmldsa65},

		{"POST", "/getNewEchCert", a.serverController.getNewEchCert},
		{"POST", "/importDB", a.serverController.importDB},
		{"POST", "/stopXrayService", a.serverController.stopXrayService},
		{"POST", "/restartXrayService", a.serverController.restartXrayService},
		{"POST", "/installXray/:version", a.serverController.installXray},
		{"POST", "/logs/:count", a.serverController.getLogs},
	}

	for _, route := range serverRoutes {
		serverApi.Handle(route.Method, route.Path, route.Handler)
	}
}

func (a *APIController) createBackup(c *gin.Context) {
	a.Tgbot.SendBackupToAdmins()
}
