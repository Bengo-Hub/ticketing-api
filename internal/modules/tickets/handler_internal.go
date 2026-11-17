package tickets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	httpware "github.com/Bengo-Hub/httpware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SourceRef names the record in the calling service a ticket was raised for. (source, type, id)
// is the idempotency key; Parent is optional context (a QA defect's session, for example) and
// not part of the key.
type SourceRef struct {
	Type   string     `json:"type"`
	ID     string     `json:"id"`
	Parent *SourceRef `json:"parent,omitempty"`
}

// metadata is the source_ref value stored in ticket metadata.
func (r SourceRef) metadata() map[string]any {
	m := map[string]any{"type": r.Type, "id": r.ID}
	if r.Parent != nil {
		m["parent"] = map[string]any{"type": r.Parent.Type, "id": r.Parent.ID}
	}
	return m
}

const maxRefLen = 200

func validRefPart(s string) bool { return s != "" && len(s) <= maxRefLen }

// validate checks a source_ref and its optional parent.
func (r SourceRef) validate() error {
	if !validRefPart(r.Type) || !validRefPart(r.ID) {
		return errors.New("source_ref.type and source_ref.id are required (at most 200 characters)")
	}
	if r.Parent != nil {
		if r.Parent.Parent != nil || !validRefPart(r.Parent.Type) || !validRefPart(r.Parent.ID) {
			return errors.New("source_ref.parent needs type and id and no parent of its own")
		}
	}
	return nil
}

// InternalCreateRequest is the body of POST /api/v1/internal/tickets.
type InternalCreateRequest struct {
	Subject        string     `json:"subject"`
	Description    string     `json:"description"`
	Priority       string     `json:"priority"`
	Channel        string     `json:"channel"`
	Category       string     `json:"category"`
	Source         string     `json:"source"`
	SourceRef      *SourceRef `json:"source_ref"`
	RequesterName  string     `json:"requester_name"`
	RequesterEmail string     `json:"requester_email"`
	RequesterPhone string     `json:"requester_phone"`
	// ContactHash is the caller's keyed hash of the contact it verified. The caller proves a later
	// reader against it; ticketing never needs to read the contact itself.
	ContactHash string `json:"contact_hash"`
}

// internalCreator is the part of Service the internal routes need.
type internalCreator interface {
	CreateInternalTicket(ctx context.Context, req *CreateTicketRequest, source string, ref *SourceRef) (*Ticket, bool, error)
	GetTicketByNumber(ctx context.Context, tenantID uuid.UUID, ticketNumber string) (*Ticket, error)
	ListComments(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID) ([]*TicketComment, error)
}

// internalTenant reads the X-Tenant-ID header every internal route requires.
func internalTenant(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(r.Header.Get(httpware.HeaderTenantID)))
	return id, err == nil && id != uuid.Nil
}

// toCreateRequest validates the body and builds the domain request.
func (in *InternalCreateRequest) toCreateRequest(tenantID uuid.UUID) (*CreateTicketRequest, error) {
	in.Subject, in.Description, in.Source = strings.TrimSpace(in.Subject), strings.TrimSpace(in.Description), strings.TrimSpace(in.Source)
	if in.Subject == "" || in.Description == "" {
		return nil, errors.New("subject and description are required")
	}
	if !validRefPart(in.Source) {
		return nil, errors.New("source is required (at most 200 characters)")
	}
	if in.SourceRef != nil {
		if err := in.SourceRef.validate(); err != nil {
			return nil, err
		}
	}
	prio := TicketPriority(in.Priority)
	switch prio {
	case "":
		prio = PriorityNormal
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent, PriorityCritical:
	default:
		return nil, errors.New("priority must be one of low, normal, high, urgent, critical")
	}
	channel := TicketChannel(in.Channel)
	if channel == "" {
		channel = ChannelWeb
	}
	if !channel.Valid() {
		return nil, errors.New("invalid channel")
	}
	meta := map[string]any{}
	if in.Category != "" {
		meta["category"] = in.Category
	}
	if in.ContactHash != "" {
		meta["contact_hash"] = in.ContactHash
	}
	req := &CreateTicketRequest{
		TenantID: tenantID, Subject: in.Subject, Description: in.Description,
		Priority: prio, Channel: channel, Metadata: meta,
		RequesterName: in.RequesterName, RequesterEmail: in.RequesterEmail, RequesterPhone: in.RequesterPhone,
	}
	if in.ContactHash != "" {
		// One contact's tickets group under one synthetic customer, as the public routes do by email.
		req.CustomerID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(in.ContactHash))
	}
	return req, nil
}

// InternalCreateTicket is the S2S create (projects-api QA defects, marketflow-ai). It needs
// X-API-Key (INTERNAL_SERVICE_KEY) and X-Tenant-ID. A repeat for the same (source,
// source_ref.type, source_ref.id) answers 200 with the existing ticket; a new ticket answers 201.
func (h *Handler) InternalCreateTicket(w http.ResponseWriter, r *http.Request) {
	if !httpware.ValidServiceKey(r, h.serviceKey) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tenantID, ok := internalTenant(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "X-Tenant-ID must be the tenant UUID")
		return
	}
	var in InternalCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := in.toCreateRequest(tenantID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	ticket, created, err := h.internal.CreateInternalTicket(r.Context(), req, in.Source, in.SourceRef)
	if err != nil {
		h.handleError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		h.logger.Info("S2S ticket created", zap.String("tenant_id", tenantID.String()),
			zap.String("ticket_number", ticket.TicketNumber), zap.String("source", in.Source))
	}
	respondJSON(w, status, map[string]any{
		"id":            ticket.ID.String(),
		"ticket_number": ticket.TicketNumber,
		"status":        ticket.Status,
	})
}

// InternalGetTicket is the S2S read of one ticket by number for a service that verified the contact
// itself (X-API-Key and X-Tenant-ID): the ticket, who may read it (contact_hash) and its public
// comments only. Internal notes and the requester's contact details never leave here.
func (h *Handler) InternalGetTicket(w http.ResponseWriter, r *http.Request) {
	if !httpware.ValidServiceKey(r, h.serviceKey) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	tenantID, ok := internalTenant(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "X-Tenant-ID must be the tenant UUID")
		return
	}
	ticket, err := h.internal.GetTicketByNumber(r.Context(), tenantID, chi.URLParam(r, "ticketNumber"))
	if err != nil {
		h.handleError(w, err)
		return
	}
	comments, err := h.internal.ListComments(r.Context(), tenantID, ticket.ID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, internalTicketView(ticket, comments))
}

// internalTicketView keeps only what a calling service may show its own user.
func internalTicketView(t *Ticket, comments []*TicketComment) map[string]any {
	public := make([]map[string]any, 0, len(comments))
	for _, c := range comments {
		if c.CommentType != CommentPublic {
			continue
		}
		public = append(public, map[string]any{"author_type": c.AuthorType, "content": c.Content, "created_at": c.CreatedAt})
	}
	return map[string]any{
		"id": t.ID.String(), "ticket_number": t.TicketNumber, "status": t.Status, "priority": t.Priority, "channel": t.Channel,
		"subject": t.Subject, "description": t.Description, "category": t.Metadata["category"], "contact_hash": t.Metadata["contact_hash"],
		"source": t.Metadata["source"], "source_ref": t.Metadata["source_ref"],
		"created_at": t.CreatedAt, "updated_at": t.UpdatedAt, "comments": public,
	}
}

// CreateInternalTicket creates a ticket for an internal caller, once per (source,
// source_ref.type, source_ref.id). created is false when the ticket already existed. Without a
// source_ref every call creates a ticket.
func (s *Service) CreateInternalTicket(ctx context.Context, req *CreateTicketRequest, source string, ref *SourceRef) (*Ticket, bool, error) {
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	req.Metadata["source"] = source
	if ref == nil {
		t, err := s.CreateTicket(ctx, req)
		return t, err == nil, err
	}
	req.Metadata["source_ref"] = ref.metadata()
	existing, err := s.repo.FindTicketBySourceRef(ctx, req.TenantID, source, *ref)
	switch {
	case err == nil:
		return existing, false, nil
	case !errors.Is(err, ErrTicketNotFound):
		return nil, false, err
	}
	t, err := s.CreateTicket(ctx, req)
	if errors.Is(err, ErrDuplicateSourceRef) {
		// A concurrent call for the same record created it first.
		existing, ferr := s.repo.FindTicketBySourceRef(ctx, req.TenantID, source, *ref)
		return existing, false, ferr
	}
	if err != nil {
		return nil, false, err
	}
	return t, true, nil
}
