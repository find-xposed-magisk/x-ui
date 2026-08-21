package controller

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

// OpenAPIPath is where the API description is published, relative to the panel
// base path.
const OpenAPIPath = APIBasePath + "/openapi.json"

// ServeOpenAPI publishes the API description. It sits behind the same session
// check as the API itself: the document is not a secret, but an unauthenticated
// copy at a well-known path would identify the host as an x-ui panel to anyone
// scanning for one.
func ServeOpenAPI(g *gin.RouterGroup, spec fs.FS) {
	document, err := fs.ReadFile(spec, "api/openapi.json")
	if err != nil {
		// The file is embedded at build time, so this cannot happen in a real
		// binary; failing loudly beats serving an empty description.
		panic("openapi document missing from the binary: " + err.Error())
	}

	base := BaseController{}
	g.GET(OpenAPIPath, base.checkLogin, func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", document)
	})
}
