package tickets

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Bengo-Hub/httpware/objectstore"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// Permission codes the ticket routes check.
const (
	permTicketsAdd       = "ticketing.tickets.add"
	permTicketsView      = "ticketing.tickets.view"
	permTicketsViewOwn   = "ticketing.tickets.view_own"
	permTicketsChange    = "ticketing.tickets.change"
	permTicketsChangeOwn = "ticketing.tickets.change_own"
	permTicketsDelete    = "ticketing.tickets.delete"
	permTicketsManage    = "ticketing.tickets.manage"
	permCommentsAdd      = "ticketing.comments.add"
	permCommentsChange   = "ticketing.comments.change"
	permCommentsChgOwn   = "ticketing.comments.change_own"
	permCommentsDelete   = "ticketing.comments.delete"
	permCommentsDelOwn   = "ticketing.comments.delete_own"
	permAttachmentsAdd   = "ticketing.attachments.add"
	permAttachmentsView  = "ticketing.attachments.view"
	permAttachViewOwn    = "ticketing.attachments.view_own"
	permAttachmentsDel   = "ticketing.attachments.delete"
	permAttachDelOwn     = "ticketing.attachments.delete_own"
	permCategoriesAdd    = "ticketing.categories.add"
	permCategoriesView   = "ticketing.categories.view"
	permCategoriesChange = "ticketing.categories.change"
	permCategoriesDelete = "ticketing.categories.delete"
	// Canned replies are curated content, managed by whoever curates the knowledge base.
	permKBManage = "ticketing.kb.manage"
)

// Handler handles HTTP requests for ticket operations.
type Handler struct {
	service    *Service
	internal   internalCreator
	presence   *Presence
	logger     *zap.Logger
	serviceKey string // INTERNAL_SERVICE_KEY for S2S calls
}

// NewHandler creates a new ticket handler.
func NewHandler(service *Service, logger *zap.Logger, opts ...func(*Handler)) *Handler {
	h := &Handler{service: service, internal: service, logger: logger.Named("tickets-handler")}
	for _, o := range opts {
		o(h)
	}
	return h
}

// WithServiceKey sets the internal service key for S2S endpoint validation.
func WithServiceKey(key string) func(*Handler) {
	return func(h *Handler) { h.serviceKey = key }
}

// WithPresence wires in collision presence.
func WithPresence(p *Presence) func(*Handler) {
	return func(h *Handler) { h.presence = p }
}

// RegisterInternalRoutes mounts the S2S routes (X-API-Key and X-Tenant-ID, no user token).
func (h *Handler) RegisterInternalRoutes(r chi.Router) {
	r.Post("/internal/tickets", h.InternalCreateTicket)
	r.Get("/internal/tickets/{ticketNumber}", h.InternalGetTicket)
}

// RegisterRoutes registers the ticket routes under the protected /{tenant} group. Routes that
// show other people's work (views, macros, presence, links, tags) need the tenant-wide
// tickets.view, which customers never hold.
func (h *Handler) RegisterRoutes(r chi.Router) {
	viewAny := access.RequireAnyPermission(permTicketsView, permTicketsViewOwn)
	staff := access.RequirePermission(permTicketsView)
	change := access.RequirePermission(permTicketsChange)
	r.Route("/tickets", func(t chi.Router) {
		t.With(access.RequirePermission(permTicketsAdd)).Post("/", h.CreateTicket)
		t.With(viewAny).Get("/", h.ListTickets)
		t.With(staff).Get("/tags", h.ListTags)
		t.With(change).Put("/bulk", h.BulkUpdate)
		t.With(viewAny).Get("/number/{ticketNumber}", h.GetTicketByNumber)
		t.With(viewAny).Get("/{ticketId}", h.GetTicket)
		t.With(access.RequireAnyPermission(permTicketsChange, permTicketsChangeOwn)).Put("/{ticketId}", h.UpdateTicket)
		t.With(access.RequireAnyPermission(permTicketsChange, permTicketsChangeOwn)).Put("/{ticketId}/status", h.UpdateTicketStatus)
		t.With(change).Put("/{ticketId}/assign", h.AssignTicket)
		t.With(access.RequirePermission(permTicketsDelete)).Delete("/{ticketId}", h.DeleteTicket)

		t.With(change).Post("/{ticketId}/merge", h.MergeTicket)
		t.With(staff).Get("/{ticketId}/links", h.ListLinks)
		t.With(change).Post("/{ticketId}/links", h.CreateLink)
		t.With(change).Delete("/{ticketId}/links/{linkId}", h.DeleteLink)
		t.With(change).Post("/{ticketId}/macros/{macroId}/apply", h.ApplyMacro)

		t.With(staff).Get("/{ticketId}/presence", h.ListPresence)
		t.With(staff).Post("/{ticketId}/presence", h.SetPresence)
		t.With(staff).Delete("/{ticketId}/presence", h.LeavePresence)

		t.With(access.RequirePermission(permCommentsAdd)).Post("/{ticketId}/comments", h.AddComment)
		t.With(viewAny).Get("/{ticketId}/comments", h.ListComments)
		t.With(access.RequireAnyPermission(permCommentsChange, permCommentsChgOwn)).Put("/{ticketId}/comments/{commentId}", h.UpdateComment)
		t.With(access.RequireAnyPermission(permCommentsDelete, permCommentsDelOwn)).Delete("/{ticketId}/comments/{commentId}", h.DeleteComment)

		attachView := access.RequireAnyPermission(permAttachmentsView, permAttachViewOwn)
		t.With(attachView).Get("/{ticketId}/attachments", h.ListAttachments)
		t.With(access.RequirePermission(permAttachmentsAdd)).Post("/{ticketId}/attachments", h.UploadAttachment)
		t.With(attachView).Get("/{ticketId}/attachments/{attachmentId}/download", h.DownloadAttachment)
		t.With(access.RequireAnyPermission(permAttachmentsDel, permAttachDelOwn)).Delete("/{ticketId}/attachments/{attachmentId}", h.DeleteAttachment)
	})

	r.Route("/views", func(v chi.Router) {
		v.Use(staff)
		v.Get("/", h.ListViews)
		v.Post("/", h.CreateView)
		v.Get("/{viewId}", h.GetView)
		v.Put("/{viewId}", h.UpdateView)
		v.Delete("/{viewId}", h.DeleteView)
	})

	r.Route("/macros", func(m chi.Router) {
		manage := access.RequirePermission(permKBManage)
		m.With(staff).Get("/", h.ListMacros)
		m.With(staff).Get("/{macroId}", h.GetMacro)
		m.With(manage).Post("/", h.CreateMacro)
		m.With(manage).Put("/{macroId}", h.UpdateMacro)
		m.With(manage).Delete("/{macroId}", h.DeleteMacro)
	})

	r.Route("/categories", func(c chi.Router) {
		c.With(access.RequirePermission(permCategoriesAdd)).Post("/", h.CreateCategory)
		c.With(access.RequirePermission(permCategoriesView)).Get("/", h.ListCategories)
		c.With(access.RequirePermission(permCategoriesView)).Get("/{categoryId}", h.GetCategory)
		c.With(access.RequirePermission(permCategoriesChange)).Put("/{categoryId}", h.UpdateCategory)
		c.With(access.RequirePermission(permCategoriesDelete)).Delete("/{categoryId}", h.DeleteCategory)
	})
}

// caller returns the request's resolved access; the protected group always sets it.
func caller(w http.ResponseWriter, r *http.Request) (*access.Access, bool) {
	a := access.From(r.Context())
	if a == nil || a.TenantID == uuid.Nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return a, true
}

// ownOnly reports whether the caller sees only their own tickets (a customer): they lack the
// tenant-wide view.
func ownOnly(a *access.Access) bool { return !a.Has(permTicketsView) }

// visibleTicket loads a ticket the caller may see. A ticket outside an own-only caller's reach is
// not found, so its existence is not revealed.
func (h *Handler) visibleTicket(w http.ResponseWriter, r *http.Request, a *access.Access, ticketID uuid.UUID) (*Ticket, bool) {
	t, err := h.service.GetTicket(r.Context(), a.TenantID, ticketID)
	if err == nil && ownOnly(a) && t.CustomerID != a.AuthUserID {
		err = ErrTicketNotFound
	}
	if err != nil {
		h.handleError(w, err)
		return nil, false
	}
	return t, true
}

// uuidParam reads a UUID URL parameter, answering 400 when it is malformed.
func uuidParam(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid "+label+" ID")
		return uuid.Nil, false
	}
	return id, true
}

func ticketIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	return uuidParam(w, r, "ticketId", "ticket")
}

// decodeJSON reads a JSON body, answering 400 when it is not valid.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		respondError(w, http.StatusBadRequest, verr.Msg)
	case objectstore.IsUploadError(err):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrTicketNotFound), errors.Is(err, ErrCommentNotFound),
		errors.Is(err, ErrAttachmentNotFound), errors.Is(err, ErrCategoryNotFound),
		errors.Is(err, ErrViewNotFound), errors.Is(err, ErrMacroNotFound), errors.Is(err, ErrLinkNotFound):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrInvalidCursor), errors.Is(err, ErrAssigneeNotFound),
		errors.Is(err, ErrMergeSelf), errors.Is(err, ErrLinkInvalid):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrForbidden):
		respondError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrCategoryExists), errors.Is(err, ErrTicketAlreadyClosed), errors.Is(err, ErrTicketCancelled),
		errors.Is(err, ErrCategoryHasChildren), errors.Is(err, ErrCategoryHasTickets), errors.Is(err, ErrDuplicateSourceRef),
		errors.Is(err, ErrAlreadyMerged), errors.Is(err, ErrMergeTargetMerged), errors.Is(err, ErrMergeTargetClosed),
		errors.Is(err, ErrMergeSourceClosed), errors.Is(err, ErrLinkExists), errors.Is(err, ErrMacroExists):
		respondError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrAgentNotAvailable), errors.Is(err, ErrStorageUnavailable),
		errors.Is(err, ErrPresenceUnavailable), errors.Is(err, objectstore.ErrScanUnavailable):
		respondError(w, http.StatusServiceUnavailable, err.Error())
	default:
		h.logger.Error("internal error", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "internal error")
	}
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
