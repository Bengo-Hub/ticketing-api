package identity

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/agent"
)

// AuthEventHandler subscribes to auth-service user events for JIT agent provisioning.
// When a user with a support role (agent/supervisor/admin) registers, an agent record
// is automatically created so they can be assigned tickets immediately after first login.
type AuthEventHandler struct {
	client *ent.Client
	logger *zap.Logger
}

// NewAuthEventHandler creates a new auth event handler.
func NewAuthEventHandler(client *ent.Client, logger *zap.Logger) *AuthEventHandler {
	return &AuthEventHandler{
		client: client,
		logger: logger.Named("identity.auth_events"),
	}
}

// authUserEvent represents the shared-events envelope for auth user events.
type authUserEvent struct {
	EventType     string                 `json:"event_type"`
	AggregateType string                 `json:"aggregate_type"`
	TenantID      uuid.UUID              `json:"tenant_id"`
	Payload       map[string]interface{} `json:"payload"`
}

// SubscribeToAuthEvents subscribes to auth.user.created via plain NATS (not JetStream).
func (h *AuthEventHandler) SubscribeToAuthEvents(nc *nats.Conn) error {
	if nc == nil {
		h.logger.Warn("NATS not available, skipping auth event subscription")
		return nil
	}

	_, err := nc.Subscribe("auth.user.created", func(msg *nats.Msg) {
		var evt authUserEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			h.logger.Error("failed to unmarshal auth.user.created", zap.Error(err))
			return
		}
		if err := h.handleUserCreated(context.Background(), &evt); err != nil {
			h.logger.Error("failed to handle auth.user.created", zap.Error(err))
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe auth.user.created: %w", err)
	}

	_, err = nc.Subscribe("auth.user.updated", func(msg *nats.Msg) {
		var evt authUserEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			h.logger.Error("failed to unmarshal auth.user.updated", zap.Error(err))
			return
		}
		if err := h.handleUserUpdated(context.Background(), &evt); err != nil {
			h.logger.Error("failed to handle auth.user.updated", zap.Error(err))
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe auth.user.updated: %w", err)
	}

	h.logger.Info("auth event subscriptions active",
		zap.Strings("subjects", []string{"auth.user.created", "auth.user.updated"}))
	return nil
}

func (h *AuthEventHandler) handleUserCreated(ctx context.Context, evt *authUserEvent) error {
	userIDStr, _ := evt.Payload["user_id"].(string)
	email, _ := evt.Payload["email"].(string)
	fullName, _ := evt.Payload["full_name"].(string)
	roleStr, _ := evt.Payload["role"].(string)

	// Only provision agents for support staff roles; customers do not get agent records.
	if !isSupportRole(roleStr) {
		return nil
	}

	authUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user_id %q: %w", userIDStr, err)
	}

	tenantID := evt.TenantID
	if tenantID == uuid.Nil {
		return fmt.Errorf("missing tenant_id for user %s", userIDStr)
	}

	// Idempotency: skip if agent already exists.
	exists, _ := h.client.Agent.Query().
		Where(agent.UserIDEQ(authUserID)).
		Exist(ctx)
	if exists {
		h.logger.Debug("agent already provisioned, skipping JIT", zap.String("user_id", userIDStr))
		return nil
	}

	agentRole := mapRole(roleStr)
	displayName := fullName
	if displayName == "" {
		displayName = email
	}

	_, err = h.client.Agent.Create().
		SetUserID(authUserID).
		SetTenantID(tenantID).
		SetEmail(email).
		SetDisplayName(displayName).
		SetRole(agentRole).
		SetStatus("offline").
		SetIsActive(true).
		SetMaxConcurrentTickets(10).
		SetCurrentTicketCount(0).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("JIT create agent for user %s: %w", userIDStr, err)
	}

	h.logger.Info("JIT agent provisioned from auth.user.created",
		zap.String("user_id", userIDStr),
		zap.String("tenant_id", tenantID.String()),
		zap.String("role", roleStr))
	return nil
}

func (h *AuthEventHandler) handleUserUpdated(ctx context.Context, evt *authUserEvent) error {
	userIDStr, _ := evt.Payload["user_id"].(string)
	email, _ := evt.Payload["email"].(string)
	fullName, _ := evt.Payload["full_name"].(string)

	authUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user_id %q: %w", userIDStr, err)
	}

	a, err := h.client.Agent.Query().
		Where(agent.UserIDEQ(authUserID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil // not a tracked support agent
		}
		return fmt.Errorf("query agent for update: %w", err)
	}

	update := h.client.Agent.UpdateOne(a)
	if email != "" {
		update = update.SetEmail(email)
	}
	if fullName != "" {
		update = update.SetDisplayName(fullName)
	}
	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("update agent from auth event: %w", err)
	}

	h.logger.Info("agent synced from auth.user.updated",
		zap.String("user_id", userIDStr))
	return nil
}

func isSupportRole(role string) bool {
	switch role {
	case "agent", "supervisor", "admin":
		return true
	}
	return false
}

func mapRole(role string) agent.Role {
	switch role {
	case "supervisor":
		return agent.RoleSupervisor
	case "admin":
		return agent.RoleAdmin
	default:
		return agent.RoleAgent
	}
}
