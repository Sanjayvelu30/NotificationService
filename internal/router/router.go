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

	v1 := r.Group("/api/v1")
	v1.Use(authMiddleware, rateLimitMiddleware)
	{
		v1.POST("/notifications", notificationHandler.Create)
		v1.GET("/notifications/:id", notificationHandler.GetByID)

		v1.POST("/templates", templateHandler.Save)
		v1.GET("/templates", templateHandler.GetAll)
	}

	return r
}
