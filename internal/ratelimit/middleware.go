package ratelimit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RateLimitMiddleware returns a Gin HandlerFunc that restricts requests using the given RateLimitSystem.
func RateLimitMiddleware(system *RateLimitSystem) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		if err := system.Allow(ip); err != nil {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": err.Error(),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
