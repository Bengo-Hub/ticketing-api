package sla

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	pagination "github.com/Bengo-Hub/pagination"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CreateCalendar creates a business calendar.
// @Summary      Create business calendar
// @Description  Create a calendar of weekly working hours and holidays. A tenant's first calendar becomes the default and is seeded with Kenyan public holidays for this year and next.
// @Tags         sla
// @Accept       json
// @Produce      json
// @Param        tenant path string true "Tenant slug"
// @Param        body   body CalendarInput true "Calendar"
// @Success      201 {object} Calendar
// @Failure      400 {object} map[string]string
// @Failure      409 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/sla/calendars [post]
func (h *Handler) CreateCalendar(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}
	var in CalendarInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.service.CreateCalendar(r.Context(), *tenantID, in)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, c)
}

// ListCalendars lists the tenant's business calendars.
// @Summary      List business calendars
// @Tags         sla
// @Produce      json
// @Param        tenant path  string true  "Tenant slug"
// @Param        page   query int    false "Page number"
// @Param        limit  query int    false "Results per page"
// @Success      200 {object} map[string]interface{}
// @Security     BearerAuth
// @Router       /{tenant}/sla/calendars [get]
func (h *Handler) ListCalendars(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}
	p := pagination.Parse(r)
	rows, total, err := h.service.ListCalendars(r.Context(), CalendarFilter{TenantID: *tenantID, Offset: p.Offset, Limit: p.Limit})
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(rows, total, p))
}

// GetCalendar returns one business calendar.
// @Summary      Get business calendar
// @Tags         sla
// @Produce      json
// @Param        tenant     path string true "Tenant slug"
// @Param        calendarId path string true "Calendar UUID"
// @Success      200 {object} Calendar
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/sla/calendars/{calendarId} [get]
func (h *Handler) GetCalendar(w http.ResponseWriter, r *http.Request) {
	tenantID, calendarID, ok := calendarParams(w, r)
	if !ok {
		return
	}
	c, err := h.service.GetCalendar(r.Context(), tenantID, calendarID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, c)
}

// UpdateCalendar changes a business calendar; omitted fields stay as they are.
// @Summary      Update business calendar
// @Tags         sla
// @Accept       json
// @Produce      json
// @Param        tenant     path string        true "Tenant slug"
// @Param        calendarId path string        true "Calendar UUID"
// @Param        body       body CalendarInput true "Fields to change"
// @Success      200 {object} Calendar
// @Failure      400 {object} map[string]string
// @Failure      404 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/sla/calendars/{calendarId} [put]
func (h *Handler) UpdateCalendar(w http.ResponseWriter, r *http.Request) {
	tenantID, calendarID, ok := calendarParams(w, r)
	if !ok {
		return
	}
	var in CalendarInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.service.UpdateCalendar(r.Context(), tenantID, calendarID, in)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, c)
}

// DeleteCalendar deletes a business calendar no policy uses.
// @Summary      Delete business calendar
// @Tags         sla
// @Param        tenant     path string true "Tenant slug"
// @Param        calendarId path string true "Calendar UUID"
// @Success      204
// @Failure      404 {object} map[string]string
// @Failure      409 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/sla/calendars/{calendarId} [delete]
func (h *Handler) DeleteCalendar(w http.ResponseWriter, r *http.Request) {
	tenantID, calendarID, ok := calendarParams(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteCalendar(r.Context(), tenantID, calendarID); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func calendarParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "calendarId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid calendar ID")
		return uuid.Nil, uuid.Nil, false
	}
	return *tenantID, id, true
}

// GetReport returns SLA compliance and timing for tickets created in a window.
// @Summary      SLA report
// @Description  Compliance, met and breached counts, and average and p90 business minutes for first response and resolution, overall, by priority and by team, for tickets created in [from, to). The window defaults to the last 30 days and is at most 366 days.
// @Tags         sla
// @Produce      json
// @Param        tenant   path  string true  "Tenant slug"
// @Param        from     query string false "Start, RFC 3339 or YYYY-MM-DD"
// @Param        to       query string false "End (exclusive), RFC 3339 or YYYY-MM-DD"
// @Param        team_id  query string false "Team UUID"
// @Param        agent_id query string false "Agent UUID"
// @Param        priority query string false "Priority"
// @Success      200 {object} Report
// @Failure      400 {object} map[string]string
// @Security     BearerAuth
// @Router       /{tenant}/sla/reports [get]
func (h *Handler) GetReport(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requestTenant(w, r)
	if !ok {
		return
	}
	f, err := parseReportFilter(r, *tenantID, time.Now())
	if err != nil {
		h.handleError(w, err)
		return
	}
	rep, err := h.reports.Report(r.Context(), f)
	if err != nil {
		h.handleError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, rep)
}

// parseReportFilter reads the report query parameters and applies the window rules.
func parseReportFilter(r *http.Request, tenantID uuid.UUID, now time.Time) (ReportFilter, error) {
	q := r.URL.Query()
	f := ReportFilter{TenantID: tenantID}
	var err error
	if f.From, err = parseInstant(q.Get("from")); err != nil {
		return f, fmt.Errorf("%w: from: %s", ErrInvalidReport, err.Error())
	}
	if f.To, err = parseInstant(q.Get("to")); err != nil {
		return f, fmt.Errorf("%w: to: %s", ErrInvalidReport, err.Error())
	}
	for key, dst := range map[string]**uuid.UUID{"team_id": &f.TeamID, "agent_id": &f.AgentID} {
		if v := q.Get(key); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				return f, fmt.Errorf("%w: %s is not a UUID", ErrInvalidReport, key)
			}
			*dst = &id
		}
	}
	if v := q.Get("priority"); v != "" {
		p := Priority(v)
		switch p {
		case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent, PriorityCritical:
			f.Priority = &p
		default:
			return f, fmt.Errorf("%w: unknown priority %q", ErrInvalidReport, v)
		}
	}
	return f, ValidateReportWindow(&f, now)
}

// parseInstant reads RFC 3339 or a UTC date; empty is the zero time.
func parseInstant(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("want RFC 3339 or YYYY-MM-DD")
	}
	return t, nil
}
