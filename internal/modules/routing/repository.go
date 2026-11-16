package routing

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines data access for routing rules.
type Repository interface {
	Create(ctx context.Context, rule *RoutingRule) error
	Get(ctx context.Context, tenantID, ruleID uuid.UUID) (*RoutingRule, error)
	List(ctx context.Context, filter RuleFilter) ([]*RoutingRule, int, error)
	Update(ctx context.Context, tenantID, ruleID uuid.UUID, req *UpdateRuleRequest) (*RoutingRule, error)
	Delete(ctx context.Context, tenantID, ruleID uuid.UUID) error
	// ListActive returns at most limit active rules in evaluation order.
	ListActive(ctx context.Context, tenantID uuid.UUID, limit int) ([]*RoutingRule, error)
}
