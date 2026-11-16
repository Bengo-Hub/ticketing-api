package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Metadata keys a merge writes.
const (
	MetaMergedInto       = "merged_into"
	MetaMergedIntoNumber = "merged_into_number"
	MetaMergedAt         = "merged_at"
	MetaMergedFrom       = "merged_from"
)

// MergeRequest is the body of POST /tickets/{id}/merge.
type MergeRequest struct {
	IntoTicketID uuid.UUID `json:"into_ticket_id"`
}

// mergedInto returns the ticket id a ticket was merged into, or "".
func mergedInto(t *Ticket) string { return metaText(t.Metadata, MetaMergedInto) }

// ValidateMerge checks that source can be merged into target. Both must be live tickets of one
// tenant (the repository guarantees it by loading them in the tenant).
func ValidateMerge(source, target *Ticket) error {
	switch {
	case source.ID == target.ID:
		return ErrMergeSelf
	case mergedInto(source) != "":
		return ErrAlreadyMerged
	case mergedInto(target) != "":
		return ErrMergeTargetMerged
	case target.Status == StatusClosed || target.Status == StatusCancelled:
		return ErrMergeTargetClosed
	case source.Status == StatusClosed || source.Status == StatusCancelled:
		return ErrMergeSourceClosed
	}
	return nil
}

// mergeMetadata is the source's metadata after the merge.
func mergeMetadata(source, target *Ticket, at time.Time) map[string]any {
	meta := copyMeta(source.Metadata)
	meta[MetaMergedInto] = target.ID.String()
	meta[MetaMergedIntoNumber] = target.TicketNumber
	meta[MetaMergedAt] = at.UTC().Format(time.RFC3339)
	return meta
}

// mergedFromMetadata is the target's metadata after the merge: merged_from lists every source.
func mergedFromMetadata(target, source *Ticket) map[string]any {
	meta := copyMeta(target.Metadata)
	var from []any
	if prev, ok := meta[MetaMergedFrom].([]any); ok {
		from = append(from, prev...)
	}
	meta[MetaMergedFrom] = append(from, source.ID.String())
	return meta
}

func copyMeta(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+3)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// MergeTicket merges source into target in one transaction: the source's comments and
// attachments move to the target, the source closes with merged_into in its metadata, the
// target takes the source's tags, and a system comment on each ticket records the merge.
func (s *Service) MergeTicket(ctx context.Context, tenantID, sourceID, targetID, actorID uuid.UUID) (*Ticket, error) {
	var target *Ticket
	err := s.repo.InTx(ctx, tenantID, func(repo Repository, out *Outbox) error {
		source, err := repo.GetTicket(ctx, tenantID, sourceID)
		if err != nil {
			return err
		}
		if target, err = repo.GetTicket(ctx, tenantID, targetID); err != nil {
			return err
		}
		if err := ValidateMerge(source, target); err != nil {
			return err
		}
		if err := repo.MoveThread(ctx, tenantID, source.ID, target.ID); err != nil {
			return err
		}
		now := s.now()
		if err := repo.UpdateTicket(ctx, tenantID, source.ID, &UpdateTicketRequest{Metadata: mergeMetadata(source, target, now)}); err != nil {
			return err
		}
		// A merge closes the source from any open status, outside the usual transitions.
		if err := repo.UpdateTicketStatus(ctx, tenantID, source.ID, StatusClosed); err != nil {
			return err
		}
		targetUpdate := &UpdateTicketRequest{Metadata: mergedFromMetadata(target, source)}
		if len(source.Tags) > 0 {
			targetUpdate.Tags = MergeTags(target.Tags, source.Tags)
		}
		if err := repo.UpdateTicket(ctx, tenantID, target.ID, targetUpdate); err != nil {
			return err
		}
		notes := []struct {
			ticket uuid.UUID
			text   string
		}{
			{source.ID, "Merged into " + target.TicketNumber + "."},
			{target.ID, source.TicketNumber + " was merged into this ticket."},
		}
		for _, n := range notes {
			if err := repo.CreateComment(ctx, tenantID, &TicketComment{
				ID: uuid.New(), TenantID: tenantID, TicketID: n.ticket, AuthorID: actorID,
				AuthorType: AuthorSystem, CommentType: CommentSystem, Content: n.text,
				Metadata: map[string]any{"merge": true, "source_id": source.ID.String(), "target_id": target.ID.String()},
			}); err != nil {
				return err
			}
		}
		out.Ticket(EventTicketMerged, TicketMergedPayload(source, target))
		target, err = repo.GetTicket(ctx, tenantID, target.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": sourceID, "status": string(StatusClosed)})
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": targetID})
	return target, nil
}

// TicketMergedPayload is ticketing.ticket.merged. It carries the source's requester so a consumer
// can tell them where their conversation continues.
func TicketMergedPayload(source, target *Ticket) map[string]any {
	p := ticketEventPayload(source)
	p["status"] = string(StatusClosed)
	p["merged_into_id"] = target.ID.String()
	p["merged_into_number"] = target.TicketNumber
	return p
}
