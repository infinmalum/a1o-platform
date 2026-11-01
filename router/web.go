package router

import (
	"embed"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/controller"
	"github.com/infinmalum/a1o-platform/middleware"
	"net/http"
	"strings"
)

// Two embedded frontends share one origin: the user-facing Studio app owns
// "/" and the trimmed operations console mounts under "/admin". Deep links
// fall back to each app's index.html so both SPAs can route internally.
func SetWebRouter(router *gin.Engine, buildFS embed.FS) {
	studioIndex, _ := buildFS.ReadFile("web/build/studio/index.html")
	adminIndex, _ := buildFS.ReadFile("web/build/admin/index.html")
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(static.Serve("/", common.EmbedFolder(buildFS, "web/build/studio")))
	router.Use(static.Serve("/admin", common.EmbedFolder(buildFS, "web/build/admin")))
	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") {
			controller.RelayNotFound(c)
			return
		}
		index := studioIndex
		if c.Request.RequestURI == "/admin" || strings.HasPrefix(c.Request.RequestURI, "/admin/") {
			index = adminIndex
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
}
