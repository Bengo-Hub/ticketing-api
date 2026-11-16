package routing

import (
	"context"
	"strings"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// maxActiveRules bounds the rules evaluated per ticket.
	maxActiveRules = 200
	// activeRulesTTL bounds how long another pod keeps evaluating a changed rule set; this pod
	// drops its copy on every change.
	activeRulesTTL = 60 * time.Second
)

// Service provides routing rule business logic.
type Service struct {
	repo   Repository
	logger *zap.Logger
	active *sharedcache.Local[uuid.UUID, []*RoutingRule]
}

// NewService creates a new routing service.
func NewService(repo Repository, logger *zap.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: logger.Named("routing"),
		active: sharedcache.NewLocal[uuid.UUID, []*RoutingRule](5000, activeRulesTTL),
	}
}

// activeRules returns the tenant's active rules, cached per tenant.
func (s *Service) activeRules(ctx context.Context, tenantID uuid.UUID) ([]*RoutingRule, error) {
	return s.active.GetOrLoad(tenantID, func() ([]*RoutingRule, error) {
		return s.repo.ListActive(ctx, tenantID, maxActiveRules)
	})
}

// CreateRule creates a new routing rule.
func (s *Service) CreateRule(ctx context.Context, req *CreateRuleRequest) (*RoutingRule, error) {
	if req.Name == "" {
		return nil, ErrInvalidRule
	}
	if len(req.Conditions) == 0 {
		return nil, ErrInvalidRule
	}
	if req.Action.Type == "" {
		return nil, ErrInvalidRule
	}

	rule := &RoutingRule{
		ID:          uuid.New(),
		TenantID:    req.TenantID,
		Name:        req.Name,
		Description: req.Description,
		IsActive:    req.IsActive,
		SortOrder:   req.SortOrder,
		Conditions:  req.Conditions,
		Action:      req.Action,
	}

	if err := s.repo.Create(ctx, rule); err != nil {
		s.logger.Error("failed to create routing rule", zap.Error(err))
		return nil, err
	}
	s.active.Delete(req.TenantID)
	return rule, nil
}

// GetRule retrieves a routing rule by ID.
func (s *Service) GetRule(ctx context.Context, tenantID, ruleID uuid.UUID) (*RoutingRule, error) {
	return s.repo.Get(ctx, tenantID, ruleID)
}

// ListRules lists routing rules for a tenant.
func (s *Service) ListRules(ctx context.Context, filter RuleFilter) ([]*RoutingRule, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	return s.repo.List(ctx, filter)
}

// UpdateRule updates a routing rule.
func (s *Service) UpdateRule(ctx context.Context, tenantID, ruleID uuid.UUID, req *UpdateRuleRequest) (*RoutingRule, error) {
	rule, err := s.repo.Update(ctx, tenantID, ruleID, req)
	s.active.Delete(tenantID)
	return rule, err
}

// DeleteRule deletes a routing rule.
func (s *Service) DeleteRule(ctx context.Context, tenantID, ruleID uuid.UUID) error {
	err := s.repo.Delete(ctx, tenantID, ruleID)
	s.active.Delete(tenantID)
	return err
}

// EvaluateRules evaluates all active routing rules against a ticket context.
// Returns the first matching rule's applied action, or nil if no rule matches.
func (s *Service) EvaluateRules(ctx context.Context, tc TicketContext) *AppliedAction {
	rules, err := s.activeRules(ctx, tc.TenantID)
	if err != nil {
		s.logger.Warn("failed to load routing rules", zap.Error(err))
		return nil
	}

	for _, rule := range rules {
		if matchesAll(rule.Conditions, tc) {
			s.logger.Info("routing rule matched",
				zap.String("rule_id", rule.ID.String()),
				zap.String("rule_name", rule.Name),
			)
			return &AppliedAction{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Action:   rule.Action,
			}
		}
	}
	return nil
}

// matchesAll checks whether ALL conditions in a rule match the ticket context (AND logic).
func matchesAll(conds []Condition, tc TicketContext) bool {
	for _, c := range conds {
		if !matchCondition(c, tc) {
			return false
		}
	}
	return true
}

func matchCondition(c Condition, tc TicketContext) bool {
	var fieldVal string
	switch c.Field {
	case FieldPriority:
		fieldVal = tc.Priority
	case FieldCategoryID:
		if tc.CategoryID != nil {
			fieldVal = tc.CategoryID.String()
		}
	case FieldChannel:
		fieldVal = tc.Channel
	case FieldSubjectContains:
		return matchOperator(c.Operator, tc.Subject, c.Value)
	case FieldDescriptionContains:
		return matchOperator(c.Operator, tc.Description, c.Value)
	default:
		return false
	}
	return matchOperator(c.Operator, fieldVal, c.Value)
}

func matchOperator(op ConditionOperator, fieldVal string, condVal any) bool {
	switch op {
	case OperatorEq:
		v, ok := condVal.(string)
		return ok && fieldVal == v
	case OperatorIn:
		switch vals := condVal.(type) {
		case []any:
			for _, v := range vals {
				if s, ok := v.(string); ok && fieldVal == s {
					return true
				}
			}
		case []string:
			for _, v := range vals {
				if fieldVal == v {
					return true
				}
			}
		}
		return false
	case OperatorContains:
		v, ok := condVal.(string)
		return ok && strings.Contains(strings.ToLower(fieldVal), strings.ToLower(v))
	default:
		return false
	}
}
