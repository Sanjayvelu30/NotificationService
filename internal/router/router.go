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

func New(notificationHandler *handler.NotificationHandler, rateLimitMiddleware gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())

	// Serve the embedded static web dashboard
	subFS, err := fs.Sub(webFS, "web")
	if err == nil {
		r.StaticFS("/dashboard", http.FS(subFS))
		r.GET("/", func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, "/dashboard/")
		})
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")
	v1.Use(rateLimitMiddleware)
	{
		v1.POST("/notifications", notificationHandler.Create)
		v1.GET("/notifications/:id", notificationHandler.GetByID)
	}

	return r
}
