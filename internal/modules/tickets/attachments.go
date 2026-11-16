package tickets

import (
	"context"
	"strings"
	"time"

	"github.com/Bengo-Hub/httpware/objectstore"
	"github.com/google/uuid"
)

// FileStore is the part of objectstore.Store attachments use.
type FileStore interface {
	Enabled() bool
	NewKey(owner objectstore.Owner, mediaType, ext string) (string, error)
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Delete(ctx context.Context, ref string) error
	DownloadURL(ref, name string, ttl time.Duration) (string, error)
}

// downloadTTL is how long a signed attachment link works.
const downloadTTL = 5 * time.Minute

// Attachment metadata keys.
const (
	metaAttachmentInternal = "internal"
	metaScanStatus         = "scan_status"
	metaScannedAt          = "scanned_at"
	metaScanEngine         = "scan_engine"
)

// AttachmentUpload is a validated upload (objectstore.ReadUpload) for one ticket.
type AttachmentUpload struct {
	TenantID   uuid.UUID
	TicketID   uuid.UUID
	CommentID  *uuid.UUID
	UploaderID uuid.UUID
	// Customer uploads are always public; staff may mark an upload internal.
	Customer bool
	Internal bool
	File     *objectstore.Upload
}

// AttachmentLink is a signed download link.
type AttachmentLink struct {
	URL       string    `json:"url"`
	Filename  string    `json:"filename"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SetFileStore wires in object storage for attachments.
func (s *Service) SetFileStore(fs FileStore) { s.files = fs }

func (s *Service) storageReady() bool { return s.files != nil && s.files.Enabled() }

// AddAttachment stores the file in the private bucket and records it. An attachment on an
// internal note is internal too; a customer can attach only to their visible comments.
func (s *Service) AddAttachment(ctx context.Context, in AttachmentUpload) (*TicketAttachment, error) {
	if !s.storageReady() {
		return nil, ErrStorageUnavailable
	}
	internal := in.Internal && !in.Customer
	if in.CommentID != nil {
		c, err := s.repo.GetComment(ctx, in.TenantID, *in.CommentID)
		if err != nil {
			return nil, err
		}
		if c.TicketID != in.TicketID || (in.Customer && c.CommentType == CommentInternal) {
			return nil, ErrCommentNotFound
		}
		internal = internal || c.CommentType == CommentInternal
	}
	key, err := s.files.NewKey(objectstore.Owner{TenantID: in.TenantID.String()}, objectstore.TypeAttachments, in.File.Ext)
	if err != nil {
		return nil, err
	}
	if err := s.files.Put(ctx, key, in.File.Data, in.File.ContentType); err != nil {
		return nil, err
	}
	meta := map[string]any{metaAttachmentInternal: internal, metaScanStatus: in.File.Scan.Status}
	if in.File.Scan.Status == objectstore.ScanStatusClean {
		meta[metaScannedAt] = in.File.Scan.ScannedAt.Format(time.RFC3339)
		meta[metaScanEngine] = in.File.Scan.Engine
	}
	a := &TicketAttachment{
		ID: uuid.New(), TenantID: in.TenantID, TicketID: in.TicketID, CommentID: in.CommentID,
		Filename: cleanFilename(in.File.FileName, in.File.Ext), ContentType: in.File.ContentType,
		Size: int64(len(in.File.Data)), StoragePath: key, UploadedByID: in.UploaderID,
		Internal: internal, Metadata: meta,
	}
	if err := s.repo.CreateAttachment(ctx, in.TenantID, a); err != nil {
		// The row is the only reference to the file: remove it so nothing is orphaned.
		if delErr := s.files.Delete(context.WithoutCancel(ctx), key); delErr != nil {
			s.logger.Warn("orphaned attachment file not removed")
		}
		return nil, err
	}
	s.broadcast(in.TenantID, "attachment.created", map[string]any{"ticket_id": in.TicketID, "attachment_id": a.ID})
	return a, nil
}

// cleanFilename keeps the base name the client sent (for display and the download name), or
// names the file from its type.
func cleanFilename(name, ext string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' {
			return -1
		}
		return r
	}, name)
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	if name == "" {
		name = "attachment" + ext
	}
	return name
}

// PageAttachments returns one keyset page of a ticket's attachments, oldest first.
func (s *Service) PageAttachments(ctx context.Context, tenantID, ticketID uuid.UUID, q AttachmentQuery) (Page[*TicketAttachment], error) {
	rows, err := s.repo.ListAttachments(ctx, tenantID, ticketID, q)
	if err != nil {
		return Page[*TicketAttachment]{}, err
	}
	return pageFrom(rows, q.Page.Limit, func(a *TicketAttachment) (time.Time, uuid.UUID) { return a.CreatedAt, a.ID }), nil
}

// GetAttachment returns an attachment of the ticket; customerView hides internal ones.
func (s *Service) GetAttachment(ctx context.Context, tenantID, ticketID, attachmentID uuid.UUID, customerView bool) (*TicketAttachment, error) {
	a, err := s.repo.GetAttachment(ctx, tenantID, attachmentID)
	if err != nil {
		return nil, err
	}
	if a.TicketID != ticketID || (customerView && a.Internal) {
		return nil, ErrAttachmentNotFound
	}
	return a, nil
}

// AttachmentDownload signs a short-lived link that downloads the file under its name.
func (s *Service) AttachmentDownload(a *TicketAttachment) (*AttachmentLink, error) {
	if !s.storageReady() {
		return nil, ErrStorageUnavailable
	}
	u, err := s.files.DownloadURL(a.StoragePath, a.Filename, downloadTTL)
	if err != nil {
		return nil, err
	}
	return &AttachmentLink{URL: u, Filename: a.Filename, ExpiresAt: s.now().Add(downloadTTL).UTC()}, nil
}

// DeleteAttachment soft-deletes an attachment. The file stays in the bucket so a deletion can
// be undone; a purge of soft-deleted files is a later job.
func (s *Service) DeleteAttachment(ctx context.Context, tenantID, ticketID uuid.UUID, a *TicketAttachment) error {
	if err := s.repo.DeleteAttachment(ctx, tenantID, a.ID); err != nil {
		return err
	}
	s.broadcast(tenantID, "attachment.deleted", map[string]any{"ticket_id": ticketID, "attachment_id": a.ID})
	return nil
}
