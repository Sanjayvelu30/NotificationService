package ratelimit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RateLimitMiddleware returns a Gin HandlerFunc that restricts requests using the given RateLimitSystem.
func RateLimitMiddleware(system *RateLimitSystem) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		var key string
		if !exists {
			key = c.ClientIP()
		} else {
			key = userID.(string)
		}

		if err := system.Allow(key); err != nil {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": err.Error(),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
