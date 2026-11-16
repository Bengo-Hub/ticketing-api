package tickets

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/ent"
	"github.com/bengobox/ticketing-service/internal/ent/ticketattachment"
)

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
	if a.Metadata != nil {
		create = create.SetMetadata(a.Metadata)
	}
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

// attachmentPublic leaves out attachments whose metadata marks them internal.
func attachmentPublic(s *entsql.Selector) {
	s.Where(entsql.P(func(b *entsql.Builder) {
		b.WriteString("COALESCE((").WriteString(s.C(ticketattachment.FieldMetadata)).
			WriteString(" ->> '" + metaAttachmentInternal + "') = 'true', false) = false")
	}))
}

func (r *EntRepository) ListAttachments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, q AttachmentQuery) ([]*TicketAttachment, error) {
	if _, err := r.GetTicket(ctx, tenantID, ticketID); err != nil {
		return nil, err
	}
	query := r.client.TicketAttachment.Query().
		Where(ticketattachment.TenantID(tenantID), ticketattachment.TicketID(ticketID), ticketattachment.DeletedAtIsNil())
	if q.PublicOnly {
		query = query.Where(attachmentPublic)
	}
	if c := q.Page.After; c != nil {
		query = query.Where(func(s *entsql.Selector) {
			s.Where(afterRow(s, ticketattachment.FieldCreatedAt, ticketattachment.FieldID, c, true))
		})
	}
	rows, err := query.
		Order(ent.Asc(ticketattachment.FieldCreatedAt), ent.Asc(ticketattachment.FieldID)).
		Limit(q.Page.Limit + 1).
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

func entAttachmentToModel(a *ent.TicketAttachment) *TicketAttachment {
	internal, _ := a.Metadata[metaAttachmentInternal].(bool)
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
		Internal:     internal,
		Metadata:     a.Metadata,
		CreatedAt:    a.CreatedAt,
	}
}
