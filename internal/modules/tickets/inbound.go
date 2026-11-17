package tickets

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Channel messages (Sprint 11). Every inbound email or WhatsApp message is either the message a
// ticket was opened from or a customer comment on one. Both carry the message's key, so a
// redelivered message is found and never stored twice:
//   - a ticket keeps it as metadata source and source_ref, under the ticket_tenant_source_ref
//     unique index the internal create already uses;
//   - a comment keeps it as metadata channel and external_id, under the unique expression index
//     ticketcomment_tenant_channel_external_id (migration 20261117120000_sprint11.sql).

// Ticket metadata keys the channel ingest writes.
const (
	// MetaEmailMessageIDs holds the Message-IDs of the customer's emails on the ticket, oldest
	// first, without angle brackets, at most maxThreadIDs. Outgoing mail answers the last one.
	MetaEmailMessageIDs = "email_message_ids"
	// MetaLastInboundAt is when the customer last wrote on any channel.
	MetaLastInboundAt = "last_inbound_at"
	// MetaFollowUpOf is the ticket number a new ticket follows up (a reply to a closed ticket).
	MetaFollowUpOf = "follow_up_of"
)

// WhatsApp window keys in ticket metadata, written at WhatsApp ingest from the customer's latest
// message: when it was received, its WhatsApp message id (the message an agent reply answers) and
// the tenant number it came in on.
const (
	MetaWhatsAppLastInboundAt = "whatsapp_last_inbound_at"
	MetaWhatsAppLastMessageID = "whatsapp_last_message_id"
	MetaWhatsAppPhoneNumberID = "whatsapp_phone_number_id"
)

// WhatsAppWindow is Meta's customer service window: free-form text may go to the customer only
// within 24 hours of their last message, and only an approved template after that.
const WhatsAppWindow = 24 * time.Hour

// Comment metadata keys of a channel message.
const (
	CommentMetaChannel    = "channel"
	CommentMetaExternalID = "external_id"
	// The sender of the message and, for email, its subject, shown in the agent's thread.
	CommentMetaFromName     = "from_name"
	CommentMetaFromEmail    = "from_email"
	CommentMetaFromPhone    = "from_phone"
	CommentMetaEmailSubject = "email_subject"
)

// withChannelMessage fills the comment's channel fields from its metadata. Comments that did
// not come in on a channel are left as they are.
func withChannelMessage(c *TicketComment) *TicketComment {
	channel := metaText(c.Metadata, CommentMetaChannel)
	if channel == "" {
		return c
	}
	c.Channel = channel
	c.ExternalID = metaText(c.Metadata, CommentMetaExternalID)
	sender := CommentSender{
		Name:  metaText(c.Metadata, CommentMetaFromName),
		Email: metaText(c.Metadata, CommentMetaFromEmail),
		Phone: metaText(c.Metadata, CommentMetaFromPhone),
	}
	if sender != (CommentSender{}) {
		c.Sender = &sender
	}
	if channel == string(ChannelEmail) {
		c.EmailSubject = metaText(c.Metadata, CommentMetaEmailSubject)
	}
	return c
}

// maxThreadIDs bounds the Message-IDs kept per ticket, and so the References header of a reply.
const maxThreadIDs = 20

// MessageKey identifies one inbound message on one channel: the email Message-ID (without angle
// brackets) or the WhatsApp message id.
type MessageKey struct {
	Channel TicketChannel
	ID      string
}

// Source is the ticket metadata source of a ticket opened from a message on this channel.
// WhatsApp uses the notifications inbox's own source, so a message auto-ticketed here and the
// same message converted by an agent in the inbox dedupe against each other.
func (k MessageKey) Source() string {
	if k.Channel == ChannelWhatsApp {
		return "notifications.whatsapp"
	}
	return string(k.Channel) + ".inbound"
}

// Ref is the source_ref of a ticket opened from the message.
func (k MessageKey) Ref() SourceRef {
	if k.Channel == ChannelWhatsApp {
		return SourceRef{Type: "wa_message", ID: k.ID}
	}
	return SourceRef{Type: string(k.Channel) + "_message", ID: k.ID}
}

// InboundFile is an attachment the sending service already stored in the private bucket; only
// its key is recorded here.
type InboundFile struct {
	Key         string
	Filename    string
	ContentType string
	Size        int64
	ScanStatus  string
}

// InboundOrigin is set on a CreateTicketRequest by the channel ingest: the message the ticket is
// opened from and the files that came with it.
type InboundOrigin struct {
	Key   MessageKey
	Files []InboundFile
	// PhoneNumberID is the tenant WhatsApp number the message came in on (WhatsApp only).
	PhoneNumberID string
	// ReceivedAt is when the channel received the message; zero means now.
	ReceivedAt time.Time
}

// InboundMessage is a customer's channel message added to an existing ticket.
type InboundMessage struct {
	TenantID uuid.UUID
	TicketID uuid.UUID
	Key      MessageKey
	// From is who wrote: the requester, or a colleague of theirs on a threaded email.
	From     RequesterContact
	Content  string
	Metadata map[string]any
	Files    []InboundFile
	// PhoneNumberID is the tenant WhatsApp number the message came in on (WhatsApp only).
	PhoneNumberID string
	// ReceivedAt is when the channel received the message; zero means now.
	ReceivedAt time.Time
}

// CustomerIDFor is the customer id of a requester known only by email or phone, the same id the
// public form derives from an email.
func CustomerIDFor(c RequesterContact) uuid.UUID {
	if e := normalizeEmail(c.Email); e != "" {
		return emailToUUID(e)
	}
	return emailToUUID(normalizePhone(c.Phone))
}

// MessageIngested reports whether the message already opened a ticket or is already a comment.
func (s *Service) MessageIngested(ctx context.Context, tenantID uuid.UUID, key MessageKey) (bool, error) {
	t, err := s.TicketForMessage(ctx, tenantID, key)
	return t != nil, err
}

// TicketForMessage returns the ticket a message opened or was added to, or nil when it is new.
func (s *Service) TicketForMessage(ctx context.Context, tenantID uuid.UUID, key MessageKey) (*Ticket, error) {
	t, err := s.repo.FindTicketBySourceRef(ctx, tenantID, key.Source(), key.Ref())
	if err == nil || !errors.Is(err, ErrTicketNotFound) {
		return t, err
	}
	c, err := s.repo.FindCommentByExternalID(ctx, tenantID, string(key.Channel), key.ID)
	if errors.Is(err, ErrCommentNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.liveTicket(ctx, tenantID, c.TicketID)
}

// TicketForOwnID returns the ticket an id from one of our own emails names: the ticket id (the
// token of the ticket.created email) or a comment id (the token of an agent reply). It is nil
// when the id is neither in this tenant.
func (s *Service) TicketForOwnID(ctx context.Context, tenantID, id uuid.UUID) (*Ticket, error) {
	t, err := s.liveTicket(ctx, tenantID, id)
	if t != nil || err != nil {
		return t, err
	}
	c, err := s.repo.GetComment(ctx, tenantID, id)
	if errors.Is(err, ErrCommentNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.liveTicket(ctx, tenantID, c.TicketID)
}

// LatestChannelTicket returns the requester's most recently active ticket on the channel that
// is not closed or cancelled and saw activity since the given time, or nil.
func (s *Service) LatestChannelTicket(ctx context.Context, tenantID uuid.UUID, channel TicketChannel, phone string, since time.Time) (*Ticket, error) {
	if phone = normalizePhone(phone); phone == "" {
		return nil, nil
	}
	t, err := s.repo.LatestOpenTicketByPhone(ctx, tenantID, channel, phone, since)
	if errors.Is(err, ErrTicketNotFound) {
		return nil, nil
	}
	return t, err
}

func (s *Service) liveTicket(ctx context.Context, tenantID, id uuid.UUID) (*Ticket, error) {
	t, err := s.repo.GetTicket(ctx, tenantID, id)
	if errors.Is(err, ErrTicketNotFound) {
		return nil, nil
	}
	return t, err
}

// AddInboundMessage adds a customer's channel message to a ticket in one transaction: the
// public customer comment (refused with ErrDuplicateMessage when the message is already one),
// its files, the thread ids in the ticket metadata, and the reopen of a ticket that was waiting
// on the customer or resolved.
func (s *Service) AddInboundMessage(ctx context.Context, in InboundMessage) (*TicketComment, error) {
	var (
		comment  *TicketComment
		reopened bool
	)
	err := s.repo.InTx(ctx, in.TenantID, func(repo Repository, out *Outbox) error {
		t, err := repo.GetTicket(ctx, in.TenantID, in.TicketID)
		if err != nil {
			return err
		}
		meta := make(map[string]any, len(in.Metadata)+2)
		for k, v := range in.Metadata {
			meta[k] = v
		}
		meta[CommentMetaChannel] = string(in.Key.Channel)
		meta[CommentMetaExternalID] = in.Key.ID
		for k, v := range map[string]string{
			CommentMetaFromName:  strings.TrimSpace(in.From.Name),
			CommentMetaFromEmail: normalizeEmail(in.From.Email),
			CommentMetaFromPhone: normalizePhone(in.From.Phone),
		} {
			if _, set := meta[k]; !set && v != "" {
				meta[k] = v
			}
		}
		authorID := CustomerIDFor(in.From)
		comment, err = s.commentTx(ctx, repo, out, &AddCommentRequest{
			TenantID: in.TenantID, TicketID: in.TicketID, AuthorID: authorID,
			AuthorType: AuthorCustomer, CommentType: CommentPublic, Content: in.Content, Metadata: meta,
		})
		if err != nil {
			return err
		}
		if err := inboundFilesTx(ctx, repo, t, &comment.ID, authorID, in.Files); err != nil {
			return err
		}
		now := s.now()
		patch := map[string]any{MetaLastInboundAt: now.UTC().Format(time.RFC3339)}
		switch in.Key.Channel {
		case ChannelEmail:
			patch[MetaEmailMessageIDs] = appendThreadID(t.Metadata, in.Key.ID)
		case ChannelWhatsApp:
			for k, v := range whatsAppInbound(t.Metadata, in.Key.ID, in.PhoneNumberID, receivedAt(in.ReceivedAt, now)) {
				patch[k] = v
			}
		}
		if err := repo.MergeTicketMetadata(ctx, in.TenantID, in.TicketID, patch); err != nil {
			return err
		}
		if t.Status == StatusPendingCustomer || t.Status == StatusResolved {
			if _, err := s.statusTx(ctx, repo, out, in.TenantID, in.TicketID, StatusOpen); err != nil {
				return err
			}
			reopened = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.broadcast(in.TenantID, "comment.created", map[string]any{"ticket_id": in.TicketID, "comment_id": comment.ID})
	if reopened {
		s.broadcast(in.TenantID, "ticket.updated", map[string]any{"ticket_id": in.TicketID, "status": string(StatusOpen)})
	}
	return comment, nil
}

// withInboundOrigin stores the message key on a new ticket's metadata (and the email
// Message-ID as the thread's first id).
func withInboundOrigin(meta map[string]any, o *InboundOrigin, now time.Time) map[string]any {
	ref := o.Key.Ref()
	meta["source"] = o.Key.Source()
	meta["source_ref"] = ref.metadata()
	meta[MetaLastInboundAt] = now.UTC().Format(time.RFC3339)
	switch o.Key.Channel {
	case ChannelEmail:
		meta[MetaEmailMessageIDs] = []string{o.Key.ID}
	case ChannelWhatsApp:
		for k, v := range whatsAppInbound(nil, o.Key.ID, o.PhoneNumberID, receivedAt(o.ReceivedAt, now)) {
			meta[k] = v
		}
	}
	return meta
}

// receivedAt is when a message was received, never later than now: a sender's clock ahead of ours
// must not stretch the WhatsApp window.
func receivedAt(at, now time.Time) time.Time {
	if at.IsZero() || at.After(now) {
		return now
	}
	return at
}

// whatsAppInbound is the WhatsApp window patch for a customer message received at the given
// time, or nil when the ticket already records a later message, so a late delivery never moves
// the window back.
func whatsAppInbound(meta map[string]any, messageID, phoneNumberID string, at time.Time) map[string]any {
	if last, ok := metaTime(meta, MetaWhatsAppLastInboundAt); ok && at.Before(last) {
		return nil
	}
	p := map[string]any{
		MetaWhatsAppLastInboundAt: at.UTC().Format(time.RFC3339),
		MetaWhatsAppLastMessageID: messageID,
	}
	if phoneNumberID = strings.TrimSpace(phoneNumberID); phoneNumberID != "" {
		p[MetaWhatsAppPhoneNumberID] = phoneNumberID
	}
	return p
}

// metaTime reads an RFC 3339 time from metadata.
func metaTime(meta map[string]any, key string) (time.Time, bool) {
	s, ok := meta[key].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// WhatsAppWindowExpiresAt is when the customer service window of a WhatsApp ticket closes, 24
// hours after the customer's last WhatsApp message. It is nil for any other channel and for a
// WhatsApp ticket with no inbound message recorded.
func WhatsAppWindowExpiresAt(t *Ticket) *time.Time {
	if t == nil || t.Channel != ChannelWhatsApp {
		return nil
	}
	last, ok := metaTime(t.Metadata, MetaWhatsAppLastInboundAt)
	if !ok {
		return nil
	}
	exp := last.UTC().Add(WhatsAppWindow)
	return &exp
}

// inboundFilesTx records files the sending service already stored; they are customer files, so
// never internal.
func inboundFilesTx(ctx context.Context, repo Repository, t *Ticket, commentID *uuid.UUID, uploader uuid.UUID, files []InboundFile) error {
	for _, f := range files {
		scan := f.ScanStatus
		if scan == "" {
			scan = "unknown"
		}
		a := &TicketAttachment{
			ID: uuid.New(), TenantID: t.TenantID, TicketID: t.ID, CommentID: commentID,
			Filename: cleanFilename(f.Filename, ""), ContentType: f.ContentType, Size: f.Size,
			StoragePath: f.Key, UploadedByID: uploader,
			Metadata: map[string]any{metaAttachmentInternal: false, metaScanStatus: scan, "source": string(t.Channel) + ".inbound"},
		}
		if err := repo.CreateAttachment(ctx, t.TenantID, a); err != nil {
			return err
		}
	}
	return nil
}

// threadIDs reads the Message-IDs kept on a ticket (a JSON array of strings).
func threadIDs(meta map[string]any) []string {
	var out []string
	switch v := meta[MetaEmailMessageIDs].(type) {
	case []string:
		out = append(out, v...)
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// appendThreadID adds a Message-ID to the ticket's thread, keeping the newest maxThreadIDs.
func appendThreadID(meta map[string]any, id string) []string {
	ids := threadIDs(meta)
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	ids = append(ids, id)
	if len(ids) > maxThreadIDs {
		ids = ids[len(ids)-maxThreadIDs:]
	}
	return ids
}
