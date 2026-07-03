package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sanjay/NotificationService/internal/handler"
)

func New(notificationHandler *handler.NotificationHandler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")
	{
		v1.POST("/notifications", notificationHandler.Create)
		v1.GET("/notifications/:id", notificationHandler.GetByID)
	}

	return r
}
