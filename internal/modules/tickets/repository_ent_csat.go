package tickets

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// consumeCSATSQL marks the survey token used in one conditional statement: the hash must be the
// ticket's current one, unused and unexpired. updated_at moves, so the analytics rollup picks the
// ticket up and counts the response in its hour.
const consumeCSATSQL = `UPDATE tickets
SET metadata = jsonb_set(metadata, '{csat,used_at}', to_jsonb($4::text)), updated_at = now()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
  AND metadata -> 'csat' ->> 'token_hash' = $3
  AND NOT (metadata -> 'csat' ? 'used_at')
  AND (metadata -> 'csat' ->> 'expires_at')::timestamptz > $5`

const insertCSATSQL = `INSERT INTO csat_responses (id, tenant_id, ticket_id, score, comment, token_hash, submitted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`

func (r *EntRepository) exec() execer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

// ConsumeCSATToken implements Repository.ConsumeCSATToken.
func (r *EntRepository) ConsumeCSATToken(ctx context.Context, tenantID uuid.UUID, ticketID uuid.UUID, tokenHash string, now time.Time) (bool, error) {
	res, err := r.exec().ExecContext(ctx, consumeCSATSQL, ticketID, tenantID, tokenHash, now.UTC().Format(time.RFC3339), now)
	if err != nil {
		return false, fmt.Errorf("consume csat token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// CreateCSATResponse implements Repository.CreateCSATResponse. The unique token_hash refuses a
// second response for one token even outside the conditional update.
func (r *EntRepository) CreateCSATResponse(ctx context.Context, resp *CSATResponse) error {
	comment := sql.NullString{String: resp.Comment, Valid: resp.Comment != ""}
	_, err := r.exec().ExecContext(ctx, insertCSATSQL, resp.ID, resp.TenantID, resp.TicketID, resp.Score,
		comment, resp.TokenHash, resp.SubmittedAt)
	if err != nil {
		return fmt.Errorf("insert csat response: %w", err)
	}
	return nil
}
