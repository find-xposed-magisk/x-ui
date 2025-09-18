package controller

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/alireza0/x-ui/web/global"
	"github.com/alireza0/x-ui/web/service"

	"github.com/gin-gonic/gin"
)

var filenameRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-.]+$`)

type ServerController struct {
	BaseController

	serverService service.ServerService

	lastStatus        *service.Status
	lastGetStatusTime time.Time

	lastVersions        []string
	lastGetVersionsTime time.Time
}

func NewServerController(g *gin.RouterGroup) *ServerController {
	a := &ServerController{
		lastGetStatusTime: time.Now(),
	}
	a.initRouter(g)
	a.startTask()
	return a
}

func (a *ServerController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/server")

	g.Use(a.checkLogin)
	g.GET("/status", a.status)
	g.GET("/getDb", a.getDb)
	g.GET("/getConfigJson", a.getConfigJson)
	g.GET("/getNewmldsa65", a.getNewmldsa65)
	g.GET("/getNewVlessEnc", a.getNewVlessEnc)
	g.GET("/getXrayVersion", a.getXrayVersion)
	g.GET("/getNewX25519Cert", a.getNewX25519Cert)

	g.POST("/getNewEchCert", a.getNewEchCert)
	g.POST("/stopXrayService", a.stopXrayService)
	g.POST("/restartXrayService", a.restartXrayService)
	g.POST("/installXray/:version", a.installXray)
	g.POST("/logs/:count", a.getLogs)
	g.POST("/importDB", a.importDB)
}

func (a *ServerController) refreshStatus() {
	a.lastStatus = a.serverService.GetStatus(a.lastStatus)
}

func (a *ServerController) startTask() {
	webServer := global.GetWebServer()
	c := webServer.GetCron()
	c.AddFunc("@every 2s", func() {
		now := time.Now()
		if now.Sub(a.lastGetStatusTime) > time.Minute*3 {
			return
		}
		a.refreshStatus()
	})
}

func (a *ServerController) status(c *gin.Context) {
	a.lastGetStatusTime = time.Now()

	jsonObj(c, a.lastStatus, nil)
}

func (a *ServerController) getXrayVersion(c *gin.Context) {
	now := time.Now()
	if now.Sub(a.lastGetVersionsTime) <= time.Minute {
		jsonObj(c, a.lastVersions, nil)
		return
	}

	versions, err := a.serverService.GetXrayVersions()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "getVersion"), err)
		return
	}

	a.lastVersions = versions
	a.lastGetVersionsTime = time.Now()

	jsonObj(c, versions, nil)
}

func (a *ServerController) installXray(c *gin.Context) {
	version := c.Param("version")
	err := a.serverService.UpdateXray(version)
	jsonMsg(c, I18nWeb(c, "install")+" xray", err)
}

func (a *ServerController) stopXrayService(c *gin.Context) {
	a.lastGetStatusTime = time.Now()
	err := a.serverService.StopXrayService()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "Xray stopped", err)
}

func (a *ServerController) restartXrayService(c *gin.Context) {
	err := a.serverService.RestartXrayService()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonMsg(c, "Xray restarted", err)
}

func (a *ServerController) getLogs(c *gin.Context) {
	count := c.Param("count")
	level := c.PostForm("level")
	syslog := c.PostForm("syslog")
	logs := a.serverService.GetLogs(count, level, syslog)
	jsonObj(c, logs, nil)
}

func (a *ServerController) getConfigJson(c *gin.Context) {
	configJson, err := a.serverService.GetConfigJson()
	if err != nil {
		jsonMsg(c, "get config.json", err)
		return
	}
	jsonObj(c, configJson, nil)
}

func (a *ServerController) getDb(c *gin.Context) {
	db, err := a.serverService.GetDb()
	if err != nil {
		jsonMsg(c, "get Database", err)
		return
	}

	filename := "x-ui.db"

	if !filenameRegex.MatchString(filename) {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid filename"))
		return
	}

	// Set the headers for the response
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", "attachment; filename="+filename)

	// Write the file contents to the response
	c.Writer.Write(db)
}

func (a *ServerController) importDB(c *gin.Context) {
	// Get the file from the request body
	file, _, err := c.Request.FormFile("db")
	if err != nil {
		jsonMsg(c, "Error reading db file", err)
		return
	}
	defer file.Close()
	// Always restart Xray before return
	defer a.serverService.RestartXrayService()
	defer func() {
		a.lastGetStatusTime = time.Now()
	}()
	// Import it
	err = a.serverService.ImportDB(file)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, "Import DB", nil)
}

func (a *ServerController) getNewX25519Cert(c *gin.Context) {
	cert, err := a.serverService.GetNewX25519Cert()
	if err != nil {
		jsonMsg(c, "get x25519 certificate", err)
		return
	}
	jsonObj(c, cert, nil)
}

func (a *ServerController) getNewmldsa65(c *gin.Context) {
	cert, err := a.serverService.GetNewmldsa65()
	if err != nil {
		jsonMsg(c, "get mldsa65 certificate", err)
		return
	}
	jsonObj(c, cert, nil)
}

func (a *ServerController) getNewEchCert(c *gin.Context) {
	sni := c.PostForm("sni")
	cert, err := a.serverService.GetNewEchCert(sni)
	if err != nil {
		jsonMsg(c, "get ech certificate", err)
		return
	}
	jsonObj(c, cert, nil)
}

func (a *ServerController) getNewVlessEnc(c *gin.Context) {
	out, err := a.serverService.GetNewVlessEnc()
	if err != nil {
		jsonMsg(c, "Failed to generate vless encryption config", err)
		return
	}
	jsonObj(c, out, nil)
}
