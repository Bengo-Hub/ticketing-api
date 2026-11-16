package identity

import (
	"context"
	"encoding/json"
	"fmt"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/modules/rbac"
)

// UserProvisioner is the part of rbac.Service the auth event consumer needs.
type UserProvisioner interface {
	SyncFromEvent(ctx context.Context, id rbac.Identity) error
	FindUser(ctx context.Context, tenantID, authUserID uuid.UUID) (*rbac.User, error)
}

// AuthEventHandler keeps ticketing users and agent profiles in step with auth-api user events.
// A new user whose global roles map to a staff role is provisioned (ticketing user, role and agent
// profile) before their first sign-in; an update refreshes a user ticketing already knows. Every
// lookup is scoped to the event's tenant.
type AuthEventHandler struct {
	users  UserProvisioner
	logger *zap.Logger
}

// NewAuthEventHandler creates a new auth event handler.
func NewAuthEventHandler(users UserProvisioner, logger *zap.Logger) *AuthEventHandler {
	return &AuthEventHandler{users: users, logger: logger.Named("identity.auth_events")}
}

// authUserEvent represents the shared-events envelope for auth user events.
type authUserEvent struct {
	EventType     string         `json:"event_type"`
	AggregateType string         `json:"aggregate_type"`
	TenantID      uuid.UUID      `json:"tenant_id"`
	Payload       map[string]any `json:"payload"`
}

// authJITQueue is the core-NATS deliver group shared by both subscriptions, so each event is
// handled by one replica.
const authJITQueue = "ticketing-auth-jit"

// SubscribeToAuthEvents subscribes to auth.user.created and auth.user.updated via core NATS.
func (h *AuthEventHandler) SubscribeToAuthEvents(nc *nats.Conn) error {
	if nc == nil {
		h.logger.Warn("NATS not available, skipping auth event subscription")
		return nil
	}
	for subject, handle := range map[string]func(context.Context, *authUserEvent) error{
		"auth.user.created": h.handleUserCreated,
		"auth.user.updated": h.handleUserUpdated,
	} {
		subject, handle := subject, handle
		if _, err := eventslib.QueueSubscribe(h.logger, nc, subject, authJITQueue, func(msg *nats.Msg) {
			var evt authUserEvent
			if err := json.Unmarshal(msg.Data, &evt); err != nil {
				h.logger.Error("failed to unmarshal auth user event", zap.String("subject", subject), zap.Error(err))
				return
			}
			if err := handle(context.Background(), &evt); err != nil {
				h.logger.Error("failed to handle auth user event", zap.String("subject", subject), zap.Error(err))
			}
		}); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	h.logger.Info("auth event subscriptions active",
		zap.Strings("subjects", []string{"auth.user.created", "auth.user.updated"}))
	return nil
}

// identityFromEvent reads the user and tenant an auth user event is about.
func identityFromEvent(evt *authUserEvent) (rbac.Identity, error) {
	userIDStr, _ := evt.Payload["user_id"].(string)
	authUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		return rbac.Identity{}, fmt.Errorf("invalid user_id %q: %w", userIDStr, err)
	}
	if evt.TenantID == uuid.Nil {
		return rbac.Identity{}, fmt.Errorf("missing tenant_id for user %s", userIDStr)
	}
	email, _ := evt.Payload["email"].(string)
	name, _ := evt.Payload["full_name"].(string)
	return rbac.Identity{
		TenantID: evt.TenantID, AuthUserID: authUserID, Email: email, Name: name,
		GlobalRoles: rolesFromPayload(evt.Payload),
	}, nil
}

func (h *AuthEventHandler) handleUserCreated(ctx context.Context, evt *authUserEvent) error {
	id, err := identityFromEvent(evt)
	if err != nil {
		return err
	}
	// Customers are provisioned on their first sign-in; only staff get a profile ahead of it.
	if !rbac.IsStaffRole(rbac.MapGlobalRoles(id.GlobalRoles)) {
		return nil
	}
	if err := h.users.SyncFromEvent(ctx, id); err != nil {
		return fmt.Errorf("provision user %s: %w", id.AuthUserID, err)
	}
	h.logger.Info("ticketing user provisioned from auth.user.created",
		zap.String("user_id", id.AuthUserID.String()), zap.String("tenant_id", id.TenantID.String()))
	return nil
}

func (h *AuthEventHandler) handleUserUpdated(ctx context.Context, evt *authUserEvent) error {
	id, err := identityFromEvent(evt)
	if err != nil {
		return err
	}
	u, err := h.users.FindUser(ctx, id.TenantID, id.AuthUserID)
	if err != nil {
		return fmt.Errorf("find ticketing user: %w", err)
	}
	if u == nil {
		return nil
	}
	return h.users.SyncFromEvent(ctx, id)
}

// rolesFromPayload reads the auth `roles` array from an event payload.
func rolesFromPayload(payload map[string]any) []string {
	raw, _ := payload["roles"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
