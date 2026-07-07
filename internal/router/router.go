package router

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sanjay/NotificationService/internal/handler"
)

//go:embed web/*
var webFS embed.FS

func New(
	notificationHandler *handler.NotificationHandler,
	templateHandler *handler.TemplateHandler,
	rateLimitMiddleware gin.HandlerFunc,
	authMiddleware gin.HandlerFunc,
) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())

	// Serve the embedded static web dashboard
	subFS, err := fs.Sub(webFS, "web")
	if err == nil {
		r.StaticFS("/dashboard", http.FS(subFS))
		r.GET("/", func(c *gin.Context) {
			landingBytes, readErr := fs.ReadFile(subFS, "landing.html")
			if readErr != nil {
				c.String(http.StatusInternalServerError, "Internal Server Error loading landing page")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", landingBytes)
		})
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Webhook callbacks from Task Scheduler (bypasses Auth0 JWT validation, uses API key auth internally)
	r.POST("/api/v1/internal/notifications/:id/dispatch", notificationHandler.DispatchCallback)

	v1 := r.Group("/api/v1")
	v1.Use(authMiddleware)
	{
		// Non-rate-limited GET endpoints (allows free polling and dashboard loading)
		v1.GET("/notifications/:id", notificationHandler.GetByID)
		v1.GET("/templates", templateHandler.GetAll)

		// CRITICAL SYSTEM DESIGN DECISION:
		// We separate read (GET) queries from write (POST) mutations.
		// Since dashboard clients poll status endpoints at high frequencies (e.g. 1Hz),
		// applying rate limits universally would exhaust a user's API quota on normal UI updates.
		// Isolating rate limits to POST endpoints protects external email/SMS provider budgets from
		// spam while maintaining continuous, uninterrupted UI diagnostics.
		mutations := v1.Group("")
		mutations.Use(rateLimitMiddleware)
		{
			mutations.POST("/notifications", notificationHandler.Create)
			mutations.POST("/templates", templateHandler.Save)
		}
	}

	return r
}
