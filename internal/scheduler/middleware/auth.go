package middleware

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// APIKeyAuth intercepts HTTP requests and validates a static Bearer token if configured.
func APIKeyAuth(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiKey == "" {
			// Fail-open for local development ease, but print warning
			log.Println("[Warning] SCHEDULER_API_KEY is not set. API Key authentication is bypassed.")
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is required"})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header must be Bearer token"})
			c.Abort()
			return
		}

		token := parts[1]
		if token != apiKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or unauthorized API key"})
			c.Abort()
			return
		}

		c.Next()
	}
}
