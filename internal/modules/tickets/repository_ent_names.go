package tickets

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// The names a page of tickets or comments shows, each resolved in one query per page.

// uuidArray is ids as a Postgres array literal ('{a,b}'), cast to uuid[] in the query. A text
// argument keeps the query independent of how the driver encodes Go slices.
func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// authorNamesSQL reads both name sources through their unique (tenant_id, user) indexes.
const authorNamesSQL = `SELECT u.id, COALESCE(NULLIF(a.display_name, ''), NULLIF(tu.name, ''), '')
FROM unnest($2::uuid[]) AS u(id)
LEFT JOIN agents a ON a.tenant_id = $1 AND a.user_id = u.id
LEFT JOIN ticketing_users tu ON tu.tenant_id = $1 AND tu.auth_service_user_id = u.id`

func (r *EntRepository) AuthorNames(ctx context.Context, tenantID uuid.UUID, userIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.q().QueryContext(ctx, authorNamesSQL, tenantID, uuidArray(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if name != "" {
			out[id] = name
		}
	}
	return out, rows.Err()
}

// slaLabelsSQL joins each policy to its calendar by primary key.
const slaLabelsSQL = `SELECT p.id, p.name, COALESCE(c.name, '')
FROM sla_policies p
LEFT JOIN business_calendars c ON c.id = p.calendar_id AND c.tenant_id = p.tenant_id
WHERE p.tenant_id = $1 AND p.id = ANY($2::uuid[])`

func (r *EntRepository) SLALabels(ctx context.Context, tenantID uuid.UUID, policyIDs []uuid.UUID) (map[uuid.UUID]SLALabel, error) {
	out := make(map[uuid.UUID]SLALabel, len(policyIDs))
	if len(policyIDs) == 0 {
		return out, nil
	}
	rows, err := r.q().QueryContext(ctx, slaLabelsSQL, tenantID, uuidArray(policyIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var l SLALabel
		if err := rows.Scan(&id, &l.PolicyName, &l.CalendarName); err != nil {
			return nil, err
		}
		out[id] = l
	}
	return out, rows.Err()
}
