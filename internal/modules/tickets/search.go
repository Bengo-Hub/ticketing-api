package tickets

import (
	"strconv"
	"strings"

	pagination "github.com/Bengo-Hub/pagination"
)

// trigramMinLength is the shortest term a pg_trgm index can serve. Shorter terms match by prefix.
const trigramMinLength = 3

// SearchPlan is how one search term is matched against tickets.
type SearchPlan struct {
	// Number is the exact ticket number the term names ("TKT-000042", "tkt-42" or "42").
	Number string
	// Pattern is the ILIKE pattern for subject, description and requester, escaped.
	Pattern string
	// Prefix is true for a short term: subject and requester match from their start, and the
	// description is not searched.
	Prefix bool
}

// PlanSearch turns a normalised term (pagination.NormalizeSearch) into a SearchPlan. An empty
// term plans nothing (zero value, ok false).
func PlanSearch(term string) (SearchPlan, bool) {
	term = pagination.NormalizeSearch(term)
	if term == "" {
		return SearchPlan{}, false
	}
	plan := SearchPlan{Number: ticketNumberOf(term)}
	if pagination.MeetsMinLength(term, trigramMinLength) {
		plan.Pattern = pagination.ContainsPattern(term)
	} else {
		plan.Pattern = pagination.EscapeLike(term) + "%"
		plan.Prefix = true
	}
	return plan, true
}

// ticketNumberOf reads a ticket number from a term: "TKT-000042" and "tkt-42" and "42" all name
// TKT-000042. Anything else is matched as typed, upper-cased.
func ticketNumberOf(term string) string {
	upper := strings.ToUpper(strings.TrimSpace(term))
	digits := strings.TrimPrefix(upper, "TKT-")
	if n, err := strconv.ParseInt(digits, 10, 64); err == nil && n > 0 && digits != "" && strings.Trim(digits, "0123456789") == "" {
		return FormatTicketNumber(n)
	}
	return upper
}

// requesterSearchExpr must stay identical to the ticket_requester_trgm expression index in
// 20261116100000_sprint9.sql, or Postgres cannot use the index.
const requesterSearchExpr = `(COALESCE("metadata" ->> 'requester_email', '') || ' ' || COALESCE("metadata" ->> 'requester_name', ''))`
