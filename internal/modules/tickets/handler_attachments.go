package tickets

import (
	"net/http"

	"github.com/Bengo-Hub/httpware/objectstore"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/platform/access"
)

// ListAttachments lists a ticket's attachments a keyset page at a time, oldest first. An
// own-only caller never sees internal ones.
// @Summary      List attachments
// @Tags         tickets
// @Produce      json
// @Param        tenant   path  string true  "Tenant slug"
// @Param        ticketId path  string true  "Ticket UUID"
// @Param        cursor   query string false "next_cursor of the previous page"
// @Param        limit    query int    false "Page size (default 25, max 100)"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/attachments [get]
func (h *Handler) ListAttachments(w http.ResponseWriter, r *http.Request) {
	a, ticketID, ok := h.ticketFromURL(w, r)
	if !ok {
		return
	}
	page, err := ParsePageParams(r)
	if err != nil {
		h.handleError(w, err)
		return
	}
	res, err := h.service.PageAttachments(r.Context(), a.TenantID, ticketID, AttachmentQuery{Page: page, PublicOnly: ownOnly(a)})
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// UploadAttachment stores a file on a ticket (multipart field "file", at most 10 MB: PDF, text,
// images, Word or Excel; checked by content and scanned). Optional form fields: comment_id, and
// for staff internal=true. A customer uploads to their own tickets only.
// @Summary      Upload attachment
// @Tags         tickets
// @Accept       multipart/form-data
// @Produce      json
// @Param        tenant   path     string true  "Tenant slug"
// @Param        ticketId path     string true  "Ticket UUID"
// @Param        file     formData file   true  "The file"
// @Success      201 {object} TicketAttachment
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/attachments [post]
func (h *Handler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	a, ticketID, ok := h.ticketFromURL(w, r)
	if !ok {
		return
	}
	file, err := objectstore.ReadUpload(w, r, "file", objectstore.TypeAttachments)
	if err != nil {
		h.handleError(w, err)
		return
	}
	in := AttachmentUpload{
		TenantID: a.TenantID, TicketID: ticketID, UploaderID: a.AuthUserID,
		Customer: ownOnly(a), Internal: r.FormValue("internal") == "true", File: file,
	}
	if v := r.FormValue("comment_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid comment ID")
			return
		}
		in.CommentID = &id
	}
	att, err := h.service.AddAttachment(r.Context(), in)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, att)
}

// DownloadAttachment answers a signed link (valid 5 minutes) that downloads the file.
// @Summary      Attachment download link
// @Tags         tickets
// @Produce      json
// @Param        tenant       path string true "Tenant slug"
// @Param        ticketId     path string true "Ticket UUID"
// @Param        attachmentId path string true "Attachment UUID"
// @Success      200 {object} AttachmentLink
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/attachments/{attachmentId}/download [get]
func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	_, att, ok := h.attachmentFromURL(w, r)
	if !ok {
		return
	}
	link, err := h.service.AttachmentDownload(att)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, link)
}

// DeleteAttachment soft-deletes an attachment: any with attachments.delete, the caller's own
// with attachments.delete_own.
// @Summary      Delete attachment
// @Tags         tickets
// @Param        tenant       path string true "Tenant slug"
// @Param        ticketId     path string true "Ticket UUID"
// @Param        attachmentId path string true "Attachment UUID"
// @Success      200 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/tickets/{ticketId}/attachments/{attachmentId} [delete]
func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	a, att, ok := h.attachmentFromURL(w, r)
	if !ok {
		return
	}
	if !a.Has(permAttachmentsDel) && att.UploadedByID != a.AuthUserID {
		h.handleError(w, ErrForbidden)
		return
	}
	if err := h.service.DeleteAttachment(r.Context(), a.TenantID, att.TicketID, att); err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ticketFromURL resolves the caller and the URL's ticket, which they must be able to see.
func (h *Handler) ticketFromURL(w http.ResponseWriter, r *http.Request) (*access.Access, uuid.UUID, bool) {
	a, ok := caller(w, r)
	if !ok {
		return nil, uuid.Nil, false
	}
	ticketID, ok := ticketIDParam(w, r)
	if !ok {
		return nil, uuid.Nil, false
	}
	if _, ok := h.visibleTicket(w, r, a, ticketID); !ok {
		return nil, uuid.Nil, false
	}
	return a, ticketID, true
}

// attachmentFromURL loads the URL's attachment on a ticket the caller can see; an internal
// attachment is not found for a customer.
func (h *Handler) attachmentFromURL(w http.ResponseWriter, r *http.Request) (*access.Access, *TicketAttachment, bool) {
	a, ticketID, ok := h.ticketFromURL(w, r)
	if !ok {
		return nil, nil, false
	}
	attachmentID, ok := uuidParam(w, r, "attachmentId", "attachment")
	if !ok {
		return nil, nil, false
	}
	att, err := h.service.GetAttachment(r.Context(), a.TenantID, ticketID, attachmentID, ownOnly(a))
	if err != nil {
		h.handleError(w, err)
		return nil, nil, false
	}
	return a, att, true
}
