package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Daily counts by UTC date, one GROUP BY each: tickets created in the window by created_at, and
// tickets resolved in the window by resolved_at (served by ticket_tenant_id_created_at_id and
// ticket_tenant_id_resolved_at). No ticket rows are loaded.
const (
	createdPerDaySQL = `SELECT to_char(date_trunc('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day, count(*)
FROM tickets WHERE tenant_id = $1 AND deleted_at IS NULL AND created_at >= $2 AND created_at <= $3
GROUP BY 1`
	resolvedPerDaySQL = `SELECT to_char(date_trunc('day', resolved_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day, count(*)
FROM tickets WHERE tenant_id = $1 AND deleted_at IS NULL AND resolved_at >= $2 AND resolved_at <= $3
GROUP BY 1`
)

func (r *EntRepository) GetTicketTrend(ctx context.Context, tenantID uuid.UUID, from, to time.Time, bucketDays int) ([]TrendPoint, error) {
	created, err := r.countPerDay(ctx, createdPerDaySQL, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	resolved, err := r.countPerDay(ctx, resolvedPerDaySQL, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	return FoldTrend(from, to, bucketDays, created, resolved), nil
}

func (r *EntRepository) countPerDay(ctx context.Context, query string, tenantID uuid.UUID, from, to time.Time) (map[string]int, error) {
	rows, err := r.q().QueryContext(ctx, query, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return nil, err
		}
		out[day] = n
	}
	return out, rows.Err()
}

// FoldTrend sums daily counts (keyed YYYY-MM-DD, UTC) into buckets of bucketDays starting at
// from's UTC date, one point per bucket up to to, empty buckets included.
func FoldTrend(from, to time.Time, bucketDays int, created, resolved map[string]int) []TrendPoint {
	if bucketDays < 1 {
		bucketDays = 1
	}
	day := func(t time.Time) time.Time {
		t = t.UTC()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	start, end := day(from), day(to)
	var points []TrendPoint
	for b := start; !b.After(end); b = b.AddDate(0, 0, bucketDays) {
		p := TrendPoint{Date: b.Format("2006-01-02")}
		for d := 0; d < bucketDays; d++ {
			key := b.AddDate(0, 0, d).Format("2006-01-02")
			p.Created += created[key]
			p.Resolved += resolved[key]
		}
		points = append(points, p)
	}
	return points
}
