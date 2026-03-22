package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hemantkpr/mailmate/internal/store"
)

// Health provides health check endpoints.
type Health struct {
	db *store.Postgres
}

// NewHealth creates a Health handler.
func NewHealth(db *store.Postgres) *Health {
	return &Health{db: db}
}

// Liveness returns 200 if the server is running.
func (h *Health) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readiness returns 200 if all dependencies are reachable.
func (h *Health) Readiness(c *gin.Context) {
	if err := h.db.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"db":     err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "db": "ok"})
}
