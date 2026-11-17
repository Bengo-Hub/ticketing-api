package tickets

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/ticketattachment"
	"github.com/bengobox/ticketing-service/internal/ent/ticketcomment"
)

// Comments and attachments carry their ticket's tenant_id; every read and write filters on it.

func (r *EntRepository) CreateComment(ctx context.Context, tenantID uuid.UUID, c *TicketComment) error {
	if _, err := r.GetTicket(ctx, tenantID, c.TicketID); err != nil {
		return err
	}
	create := r.client.TicketComment.Create().
		SetID(c.ID).
		SetTenantID(tenantID).
		SetTicketID(c.TicketID).
		SetAuthorID(c.AuthorID).
		SetAuthorType(ticketcomment.AuthorType(c.AuthorType)).
		SetCommentType(ticketcomment.CommentType(c.CommentType)).
		SetContent(c.Content)
	if len(c.Mentions) > 0 {
		create = create.SetMentions(c.Mentions)
	}
	if c.Metadata != nil {
		create = create.SetMetadata(c.Metadata)
	}
	created, err := create.Save(ctx)
	if err != nil {
		if isDuplicateMessage(err) {
			return ErrDuplicateMessage
		}
		return err
	}
	c.ID = created.ID
	c.TenantID = tenantID
	c.CreatedAt = created.CreatedAt
	return nil
}

func (r *EntRepository) GetComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID) (*TicketComment, error) {
	c, err := r.client.TicketComment.Query().
		Where(ticketcomment.ID(commentID), ticketcomment.TenantID(tenantID), ticketcomment.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCommentNotFound
		}
		return nil, err
	}
	return entCommentToModel(c), nil
}

func (r *EntRepository) ListComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, q CommentQuery) ([]*TicketComment, error) {
	if _, err := r.GetTicket(ctx, tenantID, ticketID); err != nil {
		return nil, err
	}
	query := r.client.TicketComment.Query().
		Where(ticketcomment.TenantID(tenantID), ticketcomment.TicketID(ticketID), ticketcomment.DeletedAtIsNil())
	if q.PublicOnly {
		query = query.Where(ticketcomment.CommentTypeNEQ(ticketcomment.CommentTypeInternal))
	}
	asc := !q.Newest
	if c := q.Page.After; c != nil {
		query = query.Where(func(s *entsql.Selector) {
			s.Where(afterRow(s, ticketcomment.FieldCreatedAt, ticketcomment.FieldID, c, asc))
		})
	}
	order := []ticketcomment.OrderOption{ent.Asc(ticketcomment.FieldCreatedAt), ent.Asc(ticketcomment.FieldID)}
	if !asc {
		order = []ticketcomment.OrderOption{ent.Desc(ticketcomment.FieldCreatedAt), ent.Desc(ticketcomment.FieldID)}
	}
	rows, err := query.
		Order(order...).
		Limit(q.Page.Limit + 1).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*TicketComment, len(rows))
	for i, c := range rows {
		result[i] = entCommentToModel(c)
	}
	return result, nil
}

func (r *EntRepository) UpdateComment(ctx context.Context, tenantID uuid.UUID, commentID uuid.UUID, content string) error {
	n, err := r.client.TicketComment.Update().
		Where(ticketcomment.ID(commentID), ticketcomment.TenantID(tenantID), ticketcomment.DeletedAtIsNil()).
		SetContent(content).
		SetIsEdited(true).
		SetEditedAt(time.Now()).
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
	n, err := r.client.TicketComment.Update().
		Where(ticketcomment.ID(commentID), ticketcomment.TenantID(tenantID), ticketcomment.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCommentNotFound
	}
	return nil
}

// MoveThread re-parents the source ticket's comments and attachments (soft-deleted ones too, so
// nothing stays behind on a closed ticket) to the target, within the tenant.
func (r *EntRepository) MoveThread(ctx context.Context, tenantID uuid.UUID, fromTicketID, toTicketID uuid.UUID) error {
	if _, err := r.client.TicketComment.Update().
		Where(ticketcomment.TenantID(tenantID), ticketcomment.TicketID(fromTicketID)).
		SetTicketID(toTicketID).
		Save(ctx); err != nil {
		return err
	}
	_, err := r.client.TicketAttachment.Update().
		Where(ticketattachment.TenantID(tenantID), ticketattachment.TicketID(fromTicketID)).
		SetTicketID(toTicketID).
		Save(ctx)
	return err
}

func entCommentToModel(c *ent.TicketComment) *TicketComment {
	var authorID uuid.UUID
	if c.AuthorID != nil {
		authorID = *c.AuthorID
	}
	return withChannelMessage(&TicketComment{
		ID:          c.ID,
		TenantID:    c.TenantID,
		TicketID:    c.TicketID,
		AuthorID:    authorID,
		AuthorType:  AuthorType(c.AuthorType),
		CommentType: CommentType(c.CommentType),
		Content:     c.Content,
		Mentions:    c.Mentions,
		Metadata:    c.Metadata,
		CreatedAt:   c.CreatedAt,
	})
}
