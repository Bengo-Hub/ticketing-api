package tickets

import (
	"github.com/Bengo-Hub/httpware/richtext"
	"github.com/google/uuid"
)

// maxMentions bounds the agents one comment can notify.
const maxMentions = 20

// ParseMentions returns the agent ids a comment mentions, once each, in order of first mention,
// at most maxMentions. A mention is the fleet-wide Tiptap mention span the shared editor writes
// (read by httpware/richtext). Whether each id is an agent of the tenant is checked by the caller.
func ParseMentions(content string) []uuid.UUID {
	ids := richtext.Mentions(content)
	if len(ids) > maxMentions {
		ids = ids[:maxMentions]
	}
	return ids
}

// TicketMentionedPayload is ticketing.ticket.mentioned, sent once per mentioned agent. It carries
// no requester contact: it is an internal alert, and the comment may be an internal note.
func TicketMentionedPayload(t *Ticket, c *TicketComment, agentID uuid.UUID, agentEmail string) map[string]any {
	return map[string]any{
		"ticket_id":     t.ID.String(),
		"ticket_number": t.TicketNumber,
		"subject":       t.Subject,
		"comment_id":    c.ID.String(),
		"comment_type":  string(c.CommentType),
		"comment":       excerpt(c.Content, eventExcerptLen),
		"author_id":     c.AuthorID.String(),
		"agent_id":      agentID.String(),
		"agent_email":   agentEmail,
	}
}
