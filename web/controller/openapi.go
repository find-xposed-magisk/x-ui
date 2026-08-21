package controller

import (
	"io/fs"
	"net/http"

	"github.com/alireza0/x-ui/util/common"

	"github.com/gin-gonic/gin"
)

// OpenAPIPath is where the API description is published, relative to the panel
// base path.
const OpenAPIPath = APIBasePath + "/openapi.json"

// ServeOpenAPI publishes the API description. It sits behind the same session
// check as the API itself: the document is not a secret, but an unauthenticated
// copy at a well-known path would identify the host as an x-ui panel to anyone
// scanning for one.
func ServeOpenAPI(g *gin.RouterGroup, spec fs.FS) error {
	document, err := fs.ReadFile(spec, "api/openapi.json")
	if err != nil {
		// The document is embedded at build time, so a real binary always has
		// it; returning the error rather than panicking keeps a caller that
		// hands over the wrong file system from taking the panel down.
		return common.NewErrorf("openapi document is missing: %v", err)
	}

	base := BaseController{}
	g.GET(OpenAPIPath, base.checkLogin, func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", document)
	})
	return nil
}
