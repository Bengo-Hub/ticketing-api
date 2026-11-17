package tickets

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TicketView is a saved ticket list filter, private to its owner or shared with the tenant.
type TicketView struct {
	ID          uuid.UUID      `json:"id"`
	TenantID    uuid.UUID      `json:"-"`
	OwnerUserID uuid.UUID      `json:"owner_user_id"`
	Name        string         `json:"name"`
	Filters     map[string]any `json:"filters"`
	Sort        string         `json:"sort"`
	Shared      bool           `json:"shared"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// ViewInput is the body of POST /views and PUT /views/{id} (a PUT replaces the view).
type ViewInput struct {
	Name    string         `json:"name"`
	Filters map[string]any `json:"filters"`
	Sort    string         `json:"sort"`
	Shared  bool           `json:"shared"`
}

// ViewActor is who changes a view: their user id and whether they hold tickets.manage.
type ViewActor struct {
	UserID uuid.UUID
	Manage bool
}

// maxViews bounds GET /views (own and shared together).
const maxViews = 200

// Validate checks a view.
func (in *ViewInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Sort == "" {
		in.Sort = SortCreatedDesc
	}
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 100:
		return invalid("name is required (at most 100 characters)")
	case !ValidSort(in.Sort):
		return invalid("sort must be created_desc, created_asc or sla_due")
	}
	return nil
}

// CanSee reports whether the user may read the view: their own or a shared one.
func (v *TicketView) CanSee(userID uuid.UUID) bool { return v.Shared || v.OwnerUserID == userID }

// CanEdit reports whether the actor may change the view: a shared view needs tickets.manage, a
// private view is its owner's.
func (v *TicketView) CanEdit(a ViewActor) bool {
	if v.Shared {
		return a.Manage
	}
	return v.OwnerUserID == a.UserID
}

// CreateView stores a view owned by the actor. Sharing it needs tickets.manage.
func (s *Service) CreateView(ctx context.Context, tenantID uuid.UUID, a ViewActor, in ViewInput) (*TicketView, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	if in.Shared && !a.Manage {
		return nil, ErrForbidden
	}
	v := &TicketView{ID: uuid.New(), TenantID: tenantID, OwnerUserID: a.UserID, Name: in.Name,
		Filters: in.Filters, Sort: in.Sort, Shared: in.Shared}
	if err := s.repo.CreateView(ctx, tenantID, v); err != nil {
		return nil, err
	}
	return v, nil
}

// GetView returns a view the user may see; another user's private view is not found.
func (s *Service) GetView(ctx context.Context, tenantID, viewID, userID uuid.UUID) (*TicketView, error) {
	v, err := s.repo.GetView(ctx, tenantID, viewID)
	if err != nil {
		return nil, err
	}
	if !v.CanSee(userID) {
		return nil, ErrViewNotFound
	}
	return v, nil
}

// ListViews returns the user's views and the tenant's shared views.
func (s *Service) ListViews(ctx context.Context, tenantID, userID uuid.UUID) ([]*TicketView, error) {
	return s.repo.ListViews(ctx, tenantID, userID, maxViews)
}

// UpdateView replaces a view the actor may edit. Turning a private view into a shared one needs
// tickets.manage too.
func (s *Service) UpdateView(ctx context.Context, tenantID, viewID uuid.UUID, a ViewActor, in ViewInput) (*TicketView, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	v, err := s.GetView(ctx, tenantID, viewID, a.UserID)
	if err != nil {
		return nil, err
	}
	if !v.CanEdit(a) || (in.Shared && !a.Manage) {
		return nil, ErrForbidden
	}
	v.Name, v.Filters, v.Sort, v.Shared = in.Name, in.Filters, in.Sort, in.Shared
	if err := s.repo.UpdateView(ctx, tenantID, v); err != nil {
		return nil, err
	}
	return v, nil
}

// DeleteView removes a view the actor may edit.
func (s *Service) DeleteView(ctx context.Context, tenantID, viewID uuid.UUID, a ViewActor) error {
	v, err := s.GetView(ctx, tenantID, viewID, a.UserID)
	if err != nil {
		return err
	}
	if !v.CanEdit(a) {
		return ErrForbidden
	}
	return s.repo.DeleteView(ctx, tenantID, viewID)
}
