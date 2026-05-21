package tickets

import (
	"context"
	"fmt"
	"time"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/ticket"
	"github.com/bengobox/ticketing-service/internal/ent/ticketattachment"
	"github.com/bengobox/ticketing-service/internal/ent/ticketcategory"
	"github.com/bengobox/ticketing-service/internal/ent/ticketcomment"
	"github.com/google/uuid"
)

// EntRepository implements Repository using Ent ORM.
type EntRepository struct {
	client *ent.Client
}

// NewEntRepository creates a new EntRepository.
func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client}
}

// Ticket operations

func (r *EntRepository) CreateTicket(ctx context.Context, tenantID uuid.UUID, t *Ticket) error {
	create := r.client.Ticket.Create().
		SetID(t.ID).
		SetTenantID(tenantID).
		SetTicketNumber(t.TicketNumber).
		SetCustomerID(t.CustomerID).
		SetSubject(t.Subject).
		SetDescription(t.Description).
		SetStatus(ticket.Status(t.Status)).
		SetPriority(ticket.Priority(t.Priority)).
		SetChannel(ticket.Channel(t.Channel))

	if t.CategoryID != nil {
		create = create.SetCategoryID(*t.CategoryID)
	}
	if t.AssignedAgentID != nil {
		create = create.SetAssignedAgentID(*t.AssignedAgentID)
	}
	if t.AssignedTeamID != nil {
		create = create.SetAssignedTeamID(*t.AssignedTeamID)
	}
	if t.OrderID != nil {
		create = create.SetOrderID(*t.OrderID)
	}
	if t.ExternalReference != "" {
		create = create.SetExternalReference(t.ExternalReference)
	}
	if t.CRMContactID != nil {
		create = create.SetNillableCrmContactID(t.CRMContactID)
	}
	if t.CRMLeadID != nil {
		create = create.SetNillableCrmLeadID(t.CRMLeadID)
	}
	if t.Tags != nil {
		create = create.SetTags(t.Tags)
	}
	if t.Metadata != nil {
		create = create.SetMetadata(t.Metadata)
	}

	created, err := create.Save(ctx)
	if err != nil {
		return err
	}
	t.ID = created.ID
	t.CreatedAt = created.CreatedAt
	t.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *EntRepository) GetTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) (*Ticket, error) {
	t, err := r.client.Ticket.Query().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
			ticket.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}
	return entTicketToModel(t), nil
}

func (r *EntRepository) GetTicketByNumber(ctx context.Context, tenantID uuid.UUID, ticketNumber string) (*Ticket, error) {
	t, err := r.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.TicketNumber(ticketNumber),
			ticket.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}
	return entTicketToModel(t), nil
}

func (r *EntRepository) ListTickets(ctx context.Context, tenantID uuid.UUID, filter TicketFilter) ([]*Ticket, int, error) {
	query := r.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.DeletedAtIsNil(),
		)

	if filter.Status != nil {
		query = query.Where(ticket.StatusEQ(ticket.Status(*filter.Status)))
	}
	if len(filter.Statuses) > 0 {
		statuses := make([]ticket.Status, len(filter.Statuses))
		for i, s := range filter.Statuses {
			statuses[i] = ticket.Status(s)
		}
		query = query.Where(ticket.StatusIn(statuses...))
	}
	if filter.Priority != nil {
		query = query.Where(ticket.PriorityEQ(ticket.Priority(*filter.Priority)))
	}
	if filter.CustomerID != nil {
		query = query.Where(ticket.CustomerID(*filter.CustomerID))
	}
	if filter.AssignedAgentID != nil {
		query = query.Where(ticket.AssignedAgentID(*filter.AssignedAgentID))
	}
	if filter.AssignedTeamID != nil {
		query = query.Where(ticket.AssignedTeamID(*filter.AssignedTeamID))
	}
	if filter.CategoryID != nil {
		query = query.Where(ticket.CategoryID(*filter.CategoryID))
	}
	if filter.OrderID != nil {
		query = query.Where(ticket.OrderID(*filter.OrderID))
	}
	if filter.Channel != nil {
		query = query.Where(ticket.ChannelEQ(ticket.Channel(*filter.Channel)))
	}
	if filter.CreatedAfter != nil {
		query = query.Where(ticket.CreatedAtGTE(*filter.CreatedAfter))
	}
	if filter.CreatedBefore != nil {
		query = query.Where(ticket.CreatedAtLTE(*filter.CreatedBefore))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	tickets, err := query.Order(ent.Desc(ticket.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, 0, err
	}

	result := make([]*Ticket, len(tickets))
	for i, t := range tickets {
		result[i] = entTicketToModel(t)
	}
	return result, total, nil
}

func (r *EntRepository) UpdateTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, updates *UpdateTicketRequest) error {
	update := r.client.Ticket.Update().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
			ticket.DeletedAtIsNil(),
		)

	if updates.CategoryID != nil {
		update = update.SetCategoryID(*updates.CategoryID)
	}
	if updates.AssignedAgentID != nil {
		update = update.SetAssignedAgentID(*updates.AssignedAgentID)
	}
	if updates.AssignedTeamID != nil {
		update = update.SetAssignedTeamID(*updates.AssignedTeamID)
	}
	if updates.Subject != nil {
		update = update.SetSubject(*updates.Subject)
	}
	if updates.Priority != nil {
		update = update.SetPriority(ticket.Priority(*updates.Priority))
	}
	if updates.Tags != nil {
		update = update.SetTags(updates.Tags)
	}
	if updates.Metadata != nil {
		update = update.SetMetadata(updates.Metadata)
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTicketNotFound
	}
	return nil
}

func (r *EntRepository) UpdateTicketStatus(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, status TicketStatus) error {
	now := time.Now()
	update := r.client.Ticket.Update().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
			ticket.DeletedAtIsNil(),
		).
		SetStatus(ticket.Status(status))

	// Set timestamps based on status
	switch status {
	case StatusResolved:
		update = update.SetResolvedAt(now)
	case StatusClosed:
		update = update.SetClosedAt(now)
	case StatusOpen, StatusInProgress:
		// If reopening from resolved/closed, set reopened_at
		t, err := r.GetTicket(ctx, tenantID, ticketID)
		if err == nil && (t.Status == StatusResolved || t.Status == StatusClosed) {
			update = update.SetReopenedAt(now)
		}
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTicketNotFound
	}
	return nil
}

func (r *EntRepository) AssignTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, agentID *uuid.UUID, teamID *uuid.UUID) error {
	update := r.client.Ticket.Update().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
			ticket.DeletedAtIsNil(),
		)

	if agentID != nil {
		update = update.SetAssignedAgentID(*agentID)
	} else {
		update = update.ClearAssignedAgentID()
	}

	if teamID != nil {
		update = update.SetAssignedTeamID(*teamID)
	} else {
		update = update.ClearAssignedTeamID()
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTicketNotFound
	}
	return nil
}

func (r *EntRepository) DeleteTicket(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) error {
	now := time.Now()
	n, err := r.client.Ticket.Update().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
			ticket.DeletedAtIsNil(),
		).
		SetDeletedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTicketNotFound
	}
	return nil
}

// SLA tracking

func (r *EntRepository) SetSLADeadlines(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, responseDue, resolutionDue *time.Time) error {
	update := r.client.Ticket.Update().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
		)

	if responseDue != nil {
		update = update.SetSLAResponseDueAt(*responseDue)
	}
	if resolutionDue != nil {
		update = update.SetSLAResolutionDueAt(*resolutionDue)
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTicketNotFound
	}
	return nil
}

func (r *EntRepository) MarkFirstResponse(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) error {
	now := time.Now()
	n, err := r.client.Ticket.Update().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
			ticket.FirstResponseAtIsNil(),
		).
		SetFirstResponseAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		// Already has first response or ticket not found
		return nil
	}
	return nil
}

func (r *EntRepository) MarkSLABreached(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, responseBreached, resolutionBreached bool) error {
	update := r.client.Ticket.Update().
		Where(
			ticket.ID(ticketID),
			ticket.TenantID(tenantID),
		)

	if responseBreached {
		update = update.SetSLAResponseBreached(true)
	}
	if resolutionBreached {
		update = update.SetSLAResolutionBreached(true)
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTicketNotFound
	}
	return nil
}

// Comment operations
// Note: Comments don't have tenant_id directly - they inherit tenant through ticket relation

func (r *EntRepository) CreateComment(ctx context.Context, tenantID uuid.UUID, c *TicketComment) error {
	// Verify ticket belongs to tenant
	_, err := r.GetTicket(ctx, tenantID, c.TicketID)
	if err != nil {
		return err
	}

	create := r.client.TicketComment.Create().
		SetID(c.ID).
		SetTicketID(c.TicketID).
		SetAuthorID(c.AuthorID).
		SetAuthorType(ticketcomment.AuthorType(c.AuthorType)).
		SetCommentType(ticketcomment.CommentType(c.CommentType)).
		SetContent(c.Content)

	if c.Metadata != nil {
		create = create.SetMetadata(c.Metadata)
	}

	created, err := create.Save(ctx)
	if err != nil {
		return err
	}
	c.ID = created.ID
	c.CreatedAt = created.CreatedAt
	return nil
}

func (r *EntRepository) GetComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) (*TicketComment, error) {
	c, err := r.client.TicketComment.Query().
		Where(
			ticketcomment.ID(commentID),
			ticketcomment.DeletedAtIsNil(),
		).
		WithTicket().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCommentNotFound
		}
		return nil, err
	}

	// Verify tenant through ticket
	if c.Edges.Ticket != nil && c.Edges.Ticket.TenantID != tenantID {
		return nil, ErrCommentNotFound
	}

	return entCommentToModel(c, tenantID), nil
}

func (r *EntRepository) ListComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketComment, error) {
	// Verify ticket belongs to tenant
	_, err := r.GetTicket(ctx, tenantID, ticketID)
	if err != nil {
		return nil, err
	}

	comments, err := r.client.TicketComment.Query().
		Where(
			ticketcomment.TicketID(ticketID),
			ticketcomment.DeletedAtIsNil(),
		).
		Order(ent.Asc(ticketcomment.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*TicketComment, len(comments))
	for i, c := range comments {
		result[i] = entCommentToModel(c, tenantID)
	}
	return result, nil
}

func (r *EntRepository) UpdateComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID, content string) error {
	// First verify the comment exists and belongs to tenant
	_, err := r.GetComment(ctx, tenantID, commentID)
	if err != nil {
		return err
	}

	n, err := r.client.TicketComment.Update().
		Where(
			ticketcomment.ID(commentID),
			ticketcomment.DeletedAtIsNil(),
		).
		SetContent(content).
		SetIsEdited(true).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCommentNotFound
	}
	return nil
}

func (r *EntRepository) DeleteComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) error {
	// First verify the comment exists and belongs to tenant
	_, err := r.GetComment(ctx, tenantID, commentID)
	if err != nil {
		return err
	}

	now := time.Now()
	n, err := r.client.TicketComment.Update().
		Where(
			ticketcomment.ID(commentID),
			ticketcomment.DeletedAtIsNil(),
		).
		SetDeletedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCommentNotFound
	}
	return nil
}

// Attachment operations
// Note: Attachments don't have tenant_id directly - they inherit tenant through ticket relation

func (r *EntRepository) CreateAttachment(ctx context.Context, tenantID uuid.UUID, a *TicketAttachment) error {
	// Verify ticket belongs to tenant
	_, err := r.GetTicket(ctx, tenantID, a.TicketID)
	if err != nil {
		return err
	}

	create := r.client.TicketAttachment.Create().
		SetID(a.ID).
		SetTicketID(a.TicketID).
		SetFilename(a.Filename).
		SetContentType(a.ContentType).
		SetFileSize(a.Size).
		SetStorageKey(a.StoragePath).
		SetUploadedBy(a.UploadedByID)

	if a.CommentID != nil {
		create = create.SetCommentID(*a.CommentID)
	}

	created, err := create.Save(ctx)
	if err != nil {
		return err
	}
	a.ID = created.ID
	a.CreatedAt = created.CreatedAt
	return nil
}

func (r *EntRepository) GetAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) (*TicketAttachment, error) {
	a, err := r.client.TicketAttachment.Query().
		Where(
			ticketattachment.ID(attachmentID),
		).
		WithTicket().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrAttachmentNotFound
		}
		return nil, err
	}

	// Verify tenant through ticket
	if a.Edges.Ticket != nil && a.Edges.Ticket.TenantID != tenantID {
		return nil, ErrAttachmentNotFound
	}

	return entAttachmentToModel(a, tenantID), nil
}

func (r *EntRepository) ListAttachments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketAttachment, error) {
	// Verify ticket belongs to tenant
	_, err := r.GetTicket(ctx, tenantID, ticketID)
	if err != nil {
		return nil, err
	}

	attachments, err := r.client.TicketAttachment.Query().
		Where(
			ticketattachment.TicketID(ticketID),
		).
		Order(ent.Asc(ticketattachment.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*TicketAttachment, len(attachments))
	for i, a := range attachments {
		result[i] = entAttachmentToModel(a, tenantID)
	}
	return result, nil
}

func (r *EntRepository) DeleteAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) error {
	// First verify the attachment exists and belongs to tenant
	_, err := r.GetAttachment(ctx, tenantID, attachmentID)
	if err != nil {
		return err
	}

	n, err := r.client.TicketAttachment.Delete().
		Where(
			ticketattachment.ID(attachmentID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAttachmentNotFound
	}
	return nil
}

// Category operations

func (r *EntRepository) CreateCategory(ctx context.Context, tenantID uuid.UUID, c *TicketCategory) error {
	// Generate slug from name
	slug := generateSlug(c.Name)

	create := r.client.TicketCategory.Create().
		SetID(c.ID).
		SetTenantID(tenantID).
		SetName(c.Name).
		SetSlug(slug).
		SetSortOrder(c.SortOrder).
		SetIsActive(true)

	if c.Description != "" {
		create = create.SetDescription(c.Description)
	}
	if c.ParentID != nil {
		create = create.SetParentID(*c.ParentID)
	}
	if c.Icon != "" {
		create = create.SetIcon(c.Icon)
	}

	created, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ErrCategoryExists
		}
		return err
	}
	c.ID = created.ID
	c.CreatedAt = created.CreatedAt
	c.UpdatedAt = created.UpdatedAt
	return nil
}

// generateSlug creates a URL-friendly slug from a name
func generateSlug(name string) string {
	// Simple slug generation - lowercase and replace spaces with dashes
	slug := ""
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			slug += string(r)
		} else if r >= 'A' && r <= 'Z' {
			slug += string(r + 32) // lowercase
		} else if r >= '0' && r <= '9' {
			slug += string(r)
		} else if r == ' ' || r == '-' || r == '_' {
			if len(slug) > 0 && slug[len(slug)-1] != '-' {
				slug += "-"
			}
		}
	}
	return slug
}

func (r *EntRepository) GetCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (*TicketCategory, error) {
	c, err := r.client.TicketCategory.Query().
		Where(
			ticketcategory.ID(categoryID),
			ticketcategory.TenantID(tenantID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}
	return entCategoryToModel(c), nil
}

func (r *EntRepository) ListCategories(ctx context.Context, tenantID uuid.UUID, filter CategoryFilter) ([]*TicketCategory, int, error) {
	query := r.client.TicketCategory.Query().
		Where(ticketcategory.TenantID(tenantID))

	if filter.ParentID != nil {
		query = query.Where(ticketcategory.ParentID(*filter.ParentID))
	}
	if filter.IsActive != nil {
		query = query.Where(ticketcategory.IsActive(*filter.IsActive))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	categories, err := query.Order(ent.Asc(ticketcategory.FieldSortOrder)).All(ctx)
	if err != nil {
		return nil, 0, err
	}

	result := make([]*TicketCategory, len(categories))
	for i, c := range categories {
		result[i] = entCategoryToModel(c)
	}
	return result, total, nil
}

func (r *EntRepository) UpdateCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID, updates *UpdateCategoryRequest) error {
	update := r.client.TicketCategory.Update().
		Where(
			ticketcategory.ID(categoryID),
			ticketcategory.TenantID(tenantID),
		)

	if updates.ParentID != nil {
		update = update.SetParentID(*updates.ParentID)
	}
	if updates.Name != nil {
		update = update.SetName(*updates.Name)
		update = update.SetSlug(generateSlug(*updates.Name))
	}
	if updates.Description != nil {
		update = update.SetDescription(*updates.Description)
	}
	if updates.Icon != nil {
		update = update.SetIcon(*updates.Icon)
	}
	if updates.SortOrder != nil {
		update = update.SetSortOrder(*updates.SortOrder)
	}
	if updates.IsActive != nil {
		update = update.SetIsActive(*updates.IsActive)
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

func (r *EntRepository) DeleteCategory(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) error {
	// Soft delete by setting is_active to false
	n, err := r.client.TicketCategory.Update().
		Where(
			ticketcategory.ID(categoryID),
			ticketcategory.TenantID(tenantID),
		).
		SetIsActive(false).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

func (r *EntRepository) GetCategoryTicketCount(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (int, error) {
	return r.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.CategoryID(categoryID),
			ticket.DeletedAtIsNil(),
		).
		Count(ctx)
}

func (r *EntRepository) GetCategoryChildCount(ctx context.Context, tenantID uuid.UUID, categoryID uuid.UUID) (int, error) {
	return r.client.TicketCategory.Query().
		Where(
			ticketcategory.TenantID(tenantID),
			ticketcategory.ParentID(categoryID),
			ticketcategory.IsActive(true),
		).
		Count(ctx)
}

// Stats

func (r *EntRepository) GetTicketCountsByStatus(ctx context.Context, tenantID uuid.UUID) (map[TicketStatus]int, error) {
	// Get all active tickets
	tickets, err := r.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	counts := make(map[TicketStatus]int)
	for _, t := range tickets {
		counts[TicketStatus(t.Status)]++
	}
	return counts, nil
}

func (r *EntRepository) GetAgentTicketCount(ctx context.Context, tenantID uuid.UUID, agentID uuid.UUID) (int, error) {
	return r.client.Ticket.Query().
		Where(
			ticket.TenantID(tenantID),
			ticket.AssignedAgentID(agentID),
			ticket.DeletedAtIsNil(),
			ticket.StatusNotIn(ticket.StatusClosed, ticket.StatusCancelled, ticket.StatusResolved),
		).
		Count(ctx)
}

func (r *EntRepository) ListTicketsPendingBreach(ctx context.Context, now time.Time, limit int) ([]*Ticket, error) {
	tickets, err := r.client.Ticket.Query().
		Where(
			ticket.DeletedAtIsNil(),
			ticket.StatusNotIn(ticket.StatusResolved, ticket.StatusClosed, ticket.StatusCancelled),
			ticket.Or(
				ticket.And(
					ticket.SLAResponseDueAtLTE(now),
					ticket.SLAResponseBreached(false),
					ticket.FirstResponseAtIsNil(),
				),
				ticket.And(
					ticket.SLAResolutionDueAtLTE(now),
					ticket.SLAResolutionBreached(false),
				),
			),
		).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*Ticket, len(tickets))
	for i, t := range tickets {
		result[i] = entTicketToModel(t)
	}
	return result, nil
}

func (r *EntRepository) GenerateTicketNumber(ctx context.Context, tenantID uuid.UUID) (string, error) {
	// Get the current count of tickets for this tenant
	count, err := r.client.Ticket.Query().
		Where(ticket.TenantID(tenantID)).
		Count(ctx)
	if err != nil {
		return "", err
	}

	// Generate ticket number: TKT-XXXXXX (padded to 6 digits)
	return fmt.Sprintf("TKT-%06d", count+1), nil
}

// Converters

func entTicketToModel(t *ent.Ticket) *Ticket {
	return &Ticket{
		ID:                    t.ID,
		TenantID:              t.TenantID,
		TicketNumber:          t.TicketNumber,
		CustomerID:            t.CustomerID,
		CategoryID:            t.CategoryID,
		AssignedAgentID:       t.AssignedAgentID,
		AssignedTeamID:        t.AssignedTeamID,
		Subject:               t.Subject,
		Description:           t.Description,
		Status:                TicketStatus(t.Status),
		Priority:              TicketPriority(t.Priority),
		Channel:               TicketChannel(t.Channel),
		OrderID:               t.OrderID,
		ExternalReference:     t.ExternalReference,
		CRMContactID:          t.CrmContactID,
		CRMLeadID:             t.CrmLeadID,
		SLAResponseDueAt:      t.SLAResponseDueAt,
		SLAResolutionDueAt:    t.SLAResolutionDueAt,
		SLAResponseBreached:   t.SLAResponseBreached,
		SLAResolutionBreached: t.SLAResolutionBreached,
		FirstResponseAt:       t.FirstResponseAt,
		ResolvedAt:            t.ResolvedAt,
		ClosedAt:              t.ClosedAt,
		ReopenedAt:            t.ReopenedAt,
		EscalationLevel:       t.EscalationLevel,
		EscalatedAt:           t.EscalatedAt,
		Tags:                  t.Tags,
		Metadata:              t.Metadata,
		CreatedAt:             t.CreatedAt,
		UpdatedAt:             t.UpdatedAt,
	}
}

func entCommentToModel(c *ent.TicketComment, tenantID uuid.UUID) *TicketComment {
	var authorID uuid.UUID
	if c.AuthorID != nil {
		authorID = *c.AuthorID
	}
	return &TicketComment{
		ID:          c.ID,
		TenantID:    tenantID,
		TicketID:    c.TicketID,
		AuthorID:    authorID,
		AuthorType:  AuthorType(c.AuthorType),
		CommentType: CommentType(c.CommentType),
		Content:     c.Content,
		Metadata:    c.Metadata,
		CreatedAt:   c.CreatedAt,
	}
}

func entAttachmentToModel(a *ent.TicketAttachment, tenantID uuid.UUID) *TicketAttachment {
	return &TicketAttachment{
		ID:           a.ID,
		TenantID:     tenantID,
		TicketID:     a.TicketID,
		CommentID:    a.CommentID,
		Filename:     a.Filename,
		ContentType:  a.ContentType,
		Size:         a.FileSize,
		StoragePath:  a.StorageKey,
		UploadedByID: a.UploadedBy,
		CreatedAt:    a.CreatedAt,
	}
}

func entCategoryToModel(c *ent.TicketCategory) *TicketCategory {
	return &TicketCategory{
		ID:          c.ID,
		TenantID:    c.TenantID,
		ParentID:    c.ParentID,
		Name:        c.Name,
		Description: c.Description,
		Icon:        c.Icon,
		SortOrder:   c.SortOrder,
		IsActive:    c.IsActive,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}
