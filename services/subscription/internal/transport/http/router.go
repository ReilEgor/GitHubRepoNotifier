package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/middleware"
)

// NewRouter returns the Gin engine with all routes. probe serves /health and /version.
func NewRouter(h *Handler, limiter middleware.Limiter, apiKey string, probe http.Handler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Recovery(), middleware.HandleErrors())

	r.GET("/health", gin.WrapH(probe))
	r.GET("/version", gin.WrapH(probe))

	api := r.Group("/api/v1", middleware.RateLimit(limiter))
	secured := api.Group("", middleware.APIKey(apiKey))
	secured.POST("/subscribe", h.Subscribe)
	secured.GET("/subscriptions", h.List)
	api.GET("/confirm/:token", h.Confirm)
	api.GET("/unsubscribe/:token", h.Unsubscribe)
	return r
}
