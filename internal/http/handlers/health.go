package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

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

// Readiness pings Postgres, Redis and NATS. Postgres is required: without it the pod answers 503
// and leaves the load balancer. Redis and NATS are reported but do not take the pod out, because
// the service degrades without them (per-pod rate limits and caches, events held in the outbox).
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{"postgres": "ok", "redis": "ok", "nats": "ok"}
	status := http.StatusOK
	if h.db == nil {
		checks["postgres"] = "not configured"
		status = http.StatusServiceUnavailable
	} else if err := h.db.Ping(ctx); err != nil {
		checks["postgres"] = err.Error()
		status = http.StatusServiceUnavailable
	}
	if h.cache == nil {
		checks["redis"] = "not configured"
	} else if err := h.cache.Ping(ctx).Err(); err != nil {
		checks["redis"] = err.Error()
	}
	if h.events == nil {
		checks["nats"] = "not configured"
	} else if !h.events.IsConnected() {
		checks["nats"] = "not connected"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": http.StatusText(status), "checks": checks})
}
