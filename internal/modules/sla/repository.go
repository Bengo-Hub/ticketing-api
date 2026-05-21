package sla

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines the data access interface for SLA policies.
type Repository interface {
	Create(ctx context.Context, policy *Policy) error
	Get(ctx context.Context, tenantID, policyID uuid.UUID) (*Policy, error)
	List(ctx context.Context, filter PolicyFilter) ([]*Policy, int, error)
	Update(ctx context.Context, tenantID, policyID uuid.UUID, req *UpdatePolicyRequest) error
	Delete(ctx context.Context, tenantID, policyID uuid.UUID) error

	// FindMatchingPolicy returns the best-matching active policy for a given
	// priority and optional category. Lower sort_order wins.
	FindMatchingPolicy(ctx context.Context, tenantID uuid.UUID, priority Priority, categoryID *uuid.UUID) (*Policy, error)
}
