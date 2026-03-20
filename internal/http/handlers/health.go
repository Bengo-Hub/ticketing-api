package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/database"
)

// HealthHandler handles health check endpoints.
type HealthHandler struct {
	logger *zap.Logger
	db     *database.Pool
	cache  *redis.Client
	events *nats.Conn
}

// NewHealthHandler creates a new health handler.
func NewHealthHandler(logger *zap.Logger, db *database.Pool, cache *redis.Client, events *nats.Conn) *HealthHandler {
	return &HealthHandler{
		logger: logger,
		db:     db,
		cache:  cache,
		events: events,
	}
}

// Liveness returns OK if the service is alive.
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Status returns the health status of all dependencies.
func (h *HealthHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	status := map[string]interface{}{
		"service": "ticketing-api",
		"status":  "healthy",
		"checks":  map[string]string{},
	}

	checks := status["checks"].(map[string]string)

	// Check database
	if h.db != nil {
		if err := h.db.Ping(ctx); err != nil {
			checks["database"] = "unhealthy"
			status["status"] = "degraded"
		} else {
			checks["database"] = "healthy"
		}
	} else {
		checks["database"] = "not configured"
	}

	// Check cache
	if h.cache != nil {
		if err := h.cache.Ping(ctx).Err(); err != nil {
			checks["cache"] = "unhealthy"
			status["status"] = "degraded"
		} else {
			checks["cache"] = "healthy"
		}
	} else {
		checks["cache"] = "not configured"
	}

	// Check events
	if h.events != nil {
		if h.events.IsConnected() {
			checks["events"] = "healthy"
		} else {
			checks["events"] = "unhealthy"
			status["status"] = "degraded"
		}
	} else {
		checks["events"] = "not configured"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// Metrics returns Prometheus metrics endpoint (placeholder).
func (h *HealthHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	// Prometheus metrics will be handled by the prometheus/promhttp handler
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("# Metrics endpoint"))
}
