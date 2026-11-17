package tickets

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	ratelimit "github.com/Bengo-Hub/shared-ratelimit"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// csatIPLimit bounds survey calls per client IP, like the other public routes: a token is 54
// random characters, so guessing is hopeless, and the limit keeps a script from trying anyway.
var csatIPLimit = ratelimit.Options{Name: "public-csat-ip", Limit: 20, Window: time.Hour}

// csatRequest is the body of both survey routes.
type csatRequest struct {
	Token   string `json:"token"`
	Score   int    `json:"score"`
	Comment string `json:"comment"`
}

// readCSATRequest resolves the tenant, applies the IP limit and decodes the body.
func (h *PublicHandler) readCSATRequest(w http.ResponseWriter, r *http.Request) (csatRequest, bool) {
	var req csatRequest
	tenantSlug := chi.URLParam(r, "tenant")
	if h.limiter != nil {
		if ok, _ := h.limiter.Allow(r.Context(), tenantSlug+":"+ratelimit.ClientIP(r), csatIPLimit, 1); !ok {
			respondError(w, http.StatusTooManyRequests, "rate limit exceeded: too many survey requests from this IP")
			return req, false
		}
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil || req.Token == "" {
		respondError(w, http.StatusBadRequest, "token is required")
		return req, false
	}
	return req, true
}

// CheckCSAT answers whether a survey token can still be used and what it is about, without
// using it. POST /{tenant}/public/csat/check {token}
func (h *PublicHandler) CheckCSAT(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r, chi.URLParam(r, "tenant"))
	if err != nil {
		h.tenantError(w, err)
		return
	}
	req, ok := h.readCSATRequest(w, r)
	if !ok {
		return
	}
	survey, err := h.service.CheckCSAT(r.Context(), tenantID, req.Token)
	if err != nil {
		h.csatError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, survey)
}

// SubmitCSAT stores a rating (score 1 to 5, optional comment) and uses up the token.
// POST /{tenant}/public/csat {token, score, comment}
func (h *PublicHandler) SubmitCSAT(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r, chi.URLParam(r, "tenant"))
	if err != nil {
		h.tenantError(w, err)
		return
	}
	req, ok := h.readCSATRequest(w, r)
	if !ok {
		return
	}
	if err := h.service.SubmitCSAT(r.Context(), tenantID, req.Token, req.Score, req.Comment); err != nil {
		h.csatError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"status": "recorded"})
}

// csatError maps survey errors: 404 for an unknown token, 409 once answered, 410 once expired,
// 400 for a bad score or comment.
func (h *PublicHandler) csatError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCSATTokenInvalid):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrCSATTokenUsed):
		respondError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrCSATTokenExpired):
		respondError(w, http.StatusGone, err.Error())
	case errors.Is(err, ErrCSATScore), errors.Is(err, ErrCSATComment):
		respondError(w, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error("csat survey failed", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "internal error")
	}
}
