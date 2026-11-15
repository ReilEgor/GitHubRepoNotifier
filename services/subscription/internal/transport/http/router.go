package http

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/middleware"
)

// NewRouter returns the Gin engine with all routes. probe serves /health and /version.
// X-Forwarded-For is honored only when the request comes from one of trustedProxies
// (IPs or CIDRs); with none, the caller's address is the TCP peer, so the header cannot dodge the rate limit.
func NewRouter(
	h *Handler, limiter middleware.Limiter, apiKey string, trustedProxies []string, probe http.Handler,
) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	if err := r.SetTrustedProxies(trustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}
	r.Use(middleware.RequestID(), middleware.AccessLog(), middleware.Recovery(), middleware.HandleErrors())

	r.GET("/health", gin.WrapH(probe))
	r.GET("/version", gin.WrapH(probe))

	api := r.Group("/api/v1", middleware.RateLimit(limiter))
	secured := api.Group("", middleware.APIKey(apiKey))
	secured.POST("/subscribe", h.Subscribe)
	secured.GET("/subscriptions", h.List)
	api.GET("/confirm/:token", h.Confirm)
	api.GET("/unsubscribe/:token", h.Unsubscribe)
	return r, nil
}
