package tickets

import (
	"context"
	"time"

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
	if c.Metadata != nil {
		create = create.SetMetadata(c.Metadata)
	}
	created, err := create.Save(ctx)
	if err != nil {
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

func (r *EntRepository) ListComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketComment, error) {
	if _, err := r.GetTicket(ctx, tenantID, ticketID); err != nil {
		return nil, err
	}
	rows, err := r.client.TicketComment.Query().
		Where(ticketcomment.TenantID(tenantID), ticketcomment.TicketID(ticketID), ticketcomment.DeletedAtIsNil()).
		Order(ent.Asc(ticketcomment.FieldCreatedAt)).
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

func (r *EntRepository) CreateAttachment(ctx context.Context, tenantID uuid.UUID, a *TicketAttachment) error {
	if _, err := r.GetTicket(ctx, tenantID, a.TicketID); err != nil {
		return err
	}
	create := r.client.TicketAttachment.Create().
		SetID(a.ID).
		SetTenantID(tenantID).
		SetTicketID(a.TicketID).
		SetFilename(a.Filename).
		SetContentType(a.ContentType).
		SetFileSize(a.Size).
		SetStorageKey(a.StoragePath).
		SetUploadedBy(a.UploadedByID).
		SetNillableCommentID(a.CommentID)
	created, err := create.Save(ctx)
	if err != nil {
		return err
	}
	a.ID = created.ID
	a.TenantID = tenantID
	a.CreatedAt = created.CreatedAt
	return nil
}

func (r *EntRepository) GetAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) (*TicketAttachment, error) {
	a, err := r.client.TicketAttachment.Query().
		Where(ticketattachment.ID(attachmentID), ticketattachment.TenantID(tenantID), ticketattachment.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrAttachmentNotFound
		}
		return nil, err
	}
	return entAttachmentToModel(a), nil
}

func (r *EntRepository) ListAttachments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketAttachment, error) {
	if _, err := r.GetTicket(ctx, tenantID, ticketID); err != nil {
		return nil, err
	}
	rows, err := r.client.TicketAttachment.Query().
		Where(ticketattachment.TenantID(tenantID), ticketattachment.TicketID(ticketID), ticketattachment.DeletedAtIsNil()).
		Order(ent.Asc(ticketattachment.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*TicketAttachment, len(rows))
	for i, a := range rows {
		result[i] = entAttachmentToModel(a)
	}
	return result, nil
}

func (r *EntRepository) DeleteAttachment(ctx context.Context, tenantID uuid.UUID, attachmentID uuid.UUID) error {
	n, err := r.client.TicketAttachment.Update().
		Where(ticketattachment.ID(attachmentID), ticketattachment.TenantID(tenantID), ticketattachment.DeletedAtIsNil()).
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAttachmentNotFound
	}
	return nil
}

func entCommentToModel(c *ent.TicketComment) *TicketComment {
	var authorID uuid.UUID
	if c.AuthorID != nil {
		authorID = *c.AuthorID
	}
	return &TicketComment{
		ID:          c.ID,
		TenantID:    c.TenantID,
		TicketID:    c.TicketID,
		AuthorID:    authorID,
		AuthorType:  AuthorType(c.AuthorType),
		CommentType: CommentType(c.CommentType),
		Content:     c.Content,
		Metadata:    c.Metadata,
		CreatedAt:   c.CreatedAt,
	}
}

func entAttachmentToModel(a *ent.TicketAttachment) *TicketAttachment {
	return &TicketAttachment{
		ID:           a.ID,
		TenantID:     a.TenantID,
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
