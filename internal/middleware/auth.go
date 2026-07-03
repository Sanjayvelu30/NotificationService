package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type cacheEntry struct {
	userID    string
	expiresAt time.Time
}

type Auth0Middleware struct {
	auth0Domain string
	tokenCache  sync.Map
}

func NewAuth0Middleware(domain string) *Auth0Middleware {
	return &Auth0Middleware{
		auth0Domain: domain,
	}
}

func (m *Auth0Middleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
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

		// 1. Check local cache to avoid Auth0 network calls on hot paths
		if val, ok := m.tokenCache.Load(token); ok {
			entry := val.(cacheEntry)
			if time.Now().Before(entry.expiresAt) {
				c.Set("user_id", entry.userID)
				c.Next()
				return
			}
			m.tokenCache.Delete(token) // Cache expired
		}

		// 2. Cache Miss: Make HTTP call to Auth0 UserInfo endpoint to validate token
		url := "https://" + m.auth0Domain + "/userinfo"
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			slog.Error("failed to create Auth0 request", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal auth configuration error"})
			c.Abort()
			return
		}

		req.Header.Set("Authorization", "Bearer "+token)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			slog.Error("Auth0 connection failed", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Authentication provider unreachable"})
			c.Abort()
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired session token"})
			c.Abort()
			return
		}

		var userInfo map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
			slog.Error("failed to decode Auth0 userinfo response", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse user session info"})
			c.Abort()
			return
		}

		sub, ok := userInfo["sub"].(string)
		if !ok || sub == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User identity key (sub) missing from session token"})
			c.Abort()
			return
		}

		// 3. Cache the successful validation for 5 minutes
		m.tokenCache.Store(token, cacheEntry{
			userID:    sub,
			expiresAt: time.Now().Add(5 * time.Minute),
		})

		c.Set("user_id", sub)
		c.Next()
	}
}
