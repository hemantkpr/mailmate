package middleware

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RequestID adds a unique request ID to every request.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// Logger logs each request with structured logging.
func Logger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		logger.Info("request",
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.ClientIP()),
			zap.String("request_id", c.GetString("request_id")),
		)
	}
}

// Recovery recovers from panics and returns a 500 error.
func Recovery(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic recovered",
					zap.Any("error", r),
					zap.String("path", c.Request.URL.Path),
					zap.String("request_id", c.GetString("request_id")),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "Internal server error",
				})
			}
		}()
		c.Next()
	}
}

// RateLimiter implements a simple in-memory token bucket rate limiter.
func RateLimiter(requestsPerMinute int) gin.HandlerFunc {
	type client struct {
		tokens    int
		lastReset time.Time
	}
	clients := make(map[string]*client)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		cl, exists := clients[ip]
		if !exists || now.Sub(cl.lastReset) > time.Minute {
			clients[ip] = &client{tokens: requestsPerMinute - 1, lastReset: now}
			c.Next()
			return
		}

		if cl.tokens <= 0 {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}
		cl.tokens--
		c.Next()
	}
}

// ValidateTwilioSignature verifies that webhook requests come from Twilio.
func ValidateTwilioSignature(authToken, webhookURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		signature := c.GetHeader("X-Twilio-Signature")
		if signature == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "missing signature"})
			return
		}

		if err := c.Request.ParseForm(); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid form data"})
			return
		}

		data := webhookURL
		keys := make([]string, 0, len(c.Request.PostForm))
		for k := range c.Request.PostForm {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			data += k + c.Request.PostForm.Get(k)
		}

		mac := hmac.New(sha1.New, []byte(authToken))
		mac.Write([]byte(data))
		expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(signature), []byte(expected)) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid signature"})
			return
		}

		c.Next()
	}
}

// SecureHeaders adds security headers to responses.
func SecureHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// CORS adds CORS headers for the OAuth callback pages.
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// FormatWebhookURL constructs the full webhook URL for Twilio signature validation.
func FormatWebhookURL(baseURL, path string) string {
	return fmt.Sprintf("%s%s", strings.TrimRight(baseURL, "/"), path)
}
