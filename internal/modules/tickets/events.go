package tickets

import (
	"maps"
	"strings"
	"time"

	"github.com/Bengo-Hub/httpware/richtext"
	"github.com/google/uuid"
)

// Requester contact keys in ticket metadata. Every create path stores the requester the same way
// (the public form, the S2S route and the signed-in route), so events and readers look in one place.
const (
	MetaRequesterName  = "requester_name"
	MetaRequesterEmail = "requester_email"
	MetaRequesterPhone = "requester_phone"
)

// legacy keys written by the public form before 13 November 2026; read only, never written.
const (
	metaLegacyCustomerName  = "customer_name"
	metaLegacyCustomerEmail = "customer_email"
	metaLegacyCustomerPhone = "customer_phone"
)

// RequesterContact is who raised a ticket and how to reach them.
type RequesterContact struct {
	Name  string
	Email string
	Phone string
}

// metaText reads the first non-blank string among keys.
func metaText(meta map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := meta[k].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

// normalizeEmail lowercases and trims an email; an obviously malformed one is dropped.
func normalizeEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 || strings.ContainsAny(email, " <>,;") {
		return ""
	}
	return email
}

// normalizePhone keeps a leading "+" and the digits: "+254 712 345-678" becomes "+254712345678".
// notifications-api normalises it fully for its provider; this keeps the stored value clean.
func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	var b strings.Builder
	for i, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(strings.TrimPrefix(out, "+")) < 7 {
		return ""
	}
	return out
}

// RequesterOf reads a ticket's requester, including tickets the public form stored under the
// legacy customer_* keys.
func RequesterOf(t *Ticket) RequesterContact {
	if t == nil {
		return RequesterContact{}
	}
	return RequesterContact{
		Name:  metaText(t.Metadata, MetaRequesterName, metaLegacyCustomerName),
		Email: normalizeEmail(metaText(t.Metadata, MetaRequesterEmail, metaLegacyCustomerEmail)),
		Phone: normalizePhone(metaText(t.Metadata, MetaRequesterPhone, metaLegacyCustomerPhone)),
	}
}

// withRequester stores the requester in metadata under the requester_* keys (blank ones left out)
// and drops the legacy customer_* keys, so every create path stores the contact the same way.
// The explicit fields win over values already in the metadata.
func withRequester(meta map[string]any, c RequesterContact) map[string]any {
	out := make(map[string]any, len(meta)+3)
	for k, v := range meta {
		out[k] = v
	}
	merged := RequesterContact{
		Name:  firstNonBlank(c.Name, metaText(meta, MetaRequesterName, metaLegacyCustomerName)),
		Email: normalizeEmail(firstNonBlank(c.Email, metaText(meta, MetaRequesterEmail, metaLegacyCustomerEmail))),
		Phone: normalizePhone(firstNonBlank(c.Phone, metaText(meta, MetaRequesterPhone, metaLegacyCustomerPhone))),
	}
	for _, k := range []string{MetaRequesterName, MetaRequesterEmail, MetaRequesterPhone, metaLegacyCustomerName, metaLegacyCustomerEmail, metaLegacyCustomerPhone} {
		delete(out, k)
	}
	if merged.Name != "" {
		out[MetaRequesterName] = merged.Name
	}
	if merged.Email != "" {
		out[MetaRequesterEmail] = merged.Email
	}
	if merged.Phone != "" {
		out[MetaRequesterPhone] = merged.Phone
	}
	return out
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// ticketEventPayload is the part every ticket event shares: the ticket and its requester's contact.
// Keys are always present (empty when unknown) so a consumer never has to tell missing from blank.
func ticketEventPayload(t *Ticket) map[string]any {
	req := RequesterOf(t)
	p := map[string]any{
		"ticket_id":       t.ID.String(),
		"ticket_number":   t.TicketNumber,
		"subject":         t.Subject,
		"status":          string(t.Status),
		"priority":        string(t.Priority),
		"channel":         string(t.Channel),
		"source":          metaText(t.Metadata, "source"),
		"requester_name":  req.Name,
		"requester_email": req.Email,
		"requester_phone": req.Phone,
	}
	if t.AssignedAgentID != nil {
		p["agent_id"] = t.AssignedAgentID.String()
	} else {
		p["agent_id"] = ""
	}
	maps.Copy(p, emailThread(t))
	return p
}

// emailThread is what an email about the ticket needs so the customer's reply comes back to the
// same ticket:
//   - email_message_token: the local part for this email's Message-ID (a ticket or comment id,
//     set per event; empty when the email needs no fixed id);
//   - email_thread_token: the ticket id, the token of the ticket.created email and so the root
//     of the thread's References;
//   - email_in_reply_to and email_references: the customer's last Message-ID and the thread's
//     Message-IDs, oldest first, in angle brackets;
//   - email_subject_tag and email_reply_subject: "[TKT-000042]" and "Re: <subject> [TKT-000042]".
func emailThread(t *Ticket) map[string]any {
	ids := threadIDs(t.Metadata)
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, "<"+id+">")
	}
	inReplyTo := ""
	if len(refs) > 0 {
		inReplyTo = refs[len(refs)-1]
	}
	tag := ""
	if t.TicketNumber != "" {
		tag = "[" + t.TicketNumber + "]"
	}
	return map[string]any{
		"email_message_token": "",
		"email_thread_token":  t.ID.String(),
		"email_in_reply_to":   inReplyTo,
		"email_references":    refs,
		"email_subject_tag":   tag,
		"email_reply_subject": ReplySubject(t.Subject, t.TicketNumber),
	}
}

// ReplySubject is "Re: <subject> [TKT-n]", with any "Re:" prefixes and an earlier tag removed so
// the subject does not grow on every reply.
func ReplySubject(subject, ticketNumber string) string {
	s := strings.TrimSpace(subject)
	for {
		lower := strings.ToLower(s)
		if strings.HasPrefix(lower, "re:") || strings.HasPrefix(lower, "aw:") {
			s = strings.TrimSpace(s[3:])
			continue
		}
		break
	}
	if ticketNumber != "" {
		tag := "[" + ticketNumber + "]"
		s = strings.TrimSpace(strings.ReplaceAll(s, tag, ""))
		return strings.TrimSpace("Re: " + s + " " + tag)
	}
	return "Re: " + s
}

// excerpt shortens text for an event payload (a notification shows a preview, never the whole body)
// as plain text, so editor HTML never reaches an email or WhatsApp preview as tags.
func excerpt(s string, limit int) string {
	return richtext.Excerpt(s, limit)
}

// eventExcerptLen bounds the description and comment text carried on events.
const eventExcerptLen = 500

// TicketCreatedPayload is ticketing.ticket.created.
func TicketCreatedPayload(t *Ticket) map[string]any {
	p := ticketEventPayload(t)
	p["description"] = excerpt(t.Description, eventExcerptLen)
	p["created_at"] = t.CreatedAt.UTC().Format(time.RFC3339)
	// The acknowledgement email's Message-ID is the ticket id, the root every reply references.
	p["email_message_token"] = t.ID.String()
	return p
}

// whatsAppTextLimit is the most characters WhatsApp takes in one text message.
const whatsAppTextLimit = 4096

// TicketCommentAddedPayload is ticketing.ticket.comment_added: a public agent reply the requester
// should see. Internal notes and customer comments never produce it. now decides whether the
// WhatsApp window is still open.
func TicketCommentAddedPayload(t *Ticket, c *TicketComment, agentEmail string, now time.Time) map[string]any {
	p := ticketEventPayload(t)
	p["comment_id"] = c.ID.String()
	p["comment"] = excerpt(c.Content, eventExcerptLen)
	p["author_id"] = c.AuthorID.String()
	p["author_type"] = string(c.AuthorType)
	p["agent_email"] = agentEmail
	// The reply email's Message-ID is the comment id, so an answer to it finds this ticket.
	p["email_message_token"] = c.ID.String()
	maps.Copy(p, whatsAppReply(t, c, now))
	return p
}

// whatsAppReply is what notifications-api needs to answer on WhatsApp, from the tenant number the
// customer wrote to:
//   - whatsapp_window_open: the customer's last WhatsApp message is under 24 hours old, so the
//     reply may go as free-form text;
//   - whatsapp_window_expires_at: when that window closes (RFC 3339), so a late event is checked again;
//   - whatsapp_reply_to: the WhatsApp message id of that last message;
//   - phone_number_id: the tenant number it came in on;
//   - whatsapp_text: the whole reply as plain text with its line breaks, cut to WhatsApp's limit.
//
// The keys are always present and empty unless the ticket is a WhatsApp ticket with a requester
// phone and a recorded inbound message.
func whatsAppReply(t *Ticket, c *TicketComment, now time.Time) map[string]any {
	p := map[string]any{
		"whatsapp_window_open":       false,
		"whatsapp_window_expires_at": "",
		"whatsapp_reply_to":          "",
		"phone_number_id":            "",
		"whatsapp_text":              "",
	}
	if t.Channel != ChannelWhatsApp || RequesterOf(t).Phone == "" {
		return p
	}
	exp := WhatsAppWindowExpiresAt(t)
	if exp == nil {
		return p
	}
	p["whatsapp_window_open"] = now.Before(*exp)
	p["whatsapp_window_expires_at"] = exp.UTC().Format(time.RFC3339)
	p["whatsapp_reply_to"] = metaText(t.Metadata, MetaWhatsAppLastMessageID)
	p["phone_number_id"] = metaText(t.Metadata, MetaWhatsAppPhoneNumberID)
	p["whatsapp_text"] = whatsAppText(c.Content)
	return p
}

// whatsAppText is a reply as WhatsApp plain text: paragraphs and list items keep their lines, and
// a reply longer than one WhatsApp message is cut with "...".
func whatsAppText(content string) string {
	text := []rune(richtext.PlainTextBlocks(content))
	if len(text) <= whatsAppTextLimit {
		return string(text)
	}
	return strings.TrimSpace(string(text[:whatsAppTextLimit-3])) + "..."
}

// TicketStatusChangedPayload is ticketing.ticket.status_changed (every change except to resolved,
// which is ticketing.ticket.resolved).
func TicketStatusChangedPayload(t *Ticket, from, to TicketStatus) map[string]any {
	p := ticketEventPayload(t)
	p["status"] = string(to)
	p["old_status"] = string(from)
	p["new_status"] = string(to)
	return p
}

// TicketResolvedPayload is ticketing.ticket.resolved, now carrying the requester's contact so
// "your ticket is resolved" reaches the customer, not the tenant's mailbox.
func TicketResolvedPayload(t *Ticket, from TicketStatus, resolvedAt time.Time) map[string]any {
	p := TicketStatusChangedPayload(t, from, StatusResolved)
	p["resolved_at"] = resolvedAt.UTC().Format(time.RFC3339)
	return p
}

// TicketAssignedPayload is ticketing.ticket.assigned (to the agent).
func TicketAssignedPayload(t *Ticket, agentID *uuid.UUID, agentEmail string) map[string]any {
	p := ticketEventPayload(t)
	p["agent_id"] = ""
	if agentID != nil {
		p["agent_id"] = agentID.String()
	}
	p["agent_email"] = agentEmail
	return p
}

// SLABreachPayload is ticketing.sla.first_response_breached or ticketing.sla.resolution_breached.
// It goes to the assigned agent (agent_email), else the tenant's support contact; it carries no
// requester contact because the customer is never told about an internal SLA.
func SLABreachPayload(t *Ticket, agentEmail string, responseBreached, resolutionBreached bool) map[string]any {
	eventType := "sla.first_response_breached"
	if resolutionBreached {
		eventType = "sla.resolution_breached"
	}
	p := slaEventBase(t)
	p["agent_email"] = agentEmail
	p["response_breached"] = responseBreached
	p["resolution_breached"] = resolutionBreached
	p["event_type"] = eventType
	return p
}
