package access

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Bengo-Hub/httpware"
)

// RequirePermission allows the request when the caller holds the permission.
func RequirePermission(code string) func(http.Handler) http.Handler {
	return RequireAnyPermission(code)
}

// RequireAnyPermission allows the request when the caller holds any of the permissions. A
// request with no resolved access is refused, so a route mounted outside the protected group can
// never pass by accident.
func RequireAnyPermission(codes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a := From(r.Context())
			if a == nil {
				WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "code": "unauthorized"})
				return
			}
			if a.Has(codes...) {
				next.ServeHTTP(w, r)
				return
			}
			WriteJSON(w, http.StatusForbidden, map[string]any{
				"error":    "you do not have permission to perform this action",
				"code":     "forbidden",
				"required": strings.Join(codes, " | "),
			})
		})
	}
}

// OutletContext stores the optional X-Outlet-ID header in the request context. A request without
// it continues unchanged; outlet scoping is optional.
func OutletContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if outletID := httpware.OutletHeader(r); outletID != "" {
			r = r.WithContext(httpware.WithOutletID(r.Context(), outletID))
		}
		next.ServeHTTP(w, r)
	})
}

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
