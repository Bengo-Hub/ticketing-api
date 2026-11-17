package ingest

import (
	"strings"

	"github.com/Bengo-Hub/httpware/contact"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// WhatsAppInbound is notifications.whatsapp.tenant_inbound: one message to a tenant's own
// WhatsApp number, published by notifications-api.
type WhatsAppInbound struct {
	TenantID      string `json:"tenant_id"`
	PhoneNumberID string `json:"phone_number_id"`
	WAMessageID   string `json:"wa_message_id"`
	From          string `json:"from"`
	Type          string `json:"type"`
	Text          string `json:"text"`
	ContactName   string `json:"contact_name"`
	ReceivedAt    string `json:"received_at"`
	ReplyID       string `json:"reply_id"`
}

// stopWords are opt-out replies; they never open or continue a ticket.
var stopWords = map[string]bool{"stop": true, "stop all": true, "stopall": true, "unsubscribe": true, "opt out": true, "optout": true}

// WhatsAppMessage turns the event into a Message. autoTicket is the tenant's
// channels.whatsapp.auto_ticket: off, the message only continues an open ticket. ok is false
// when the event has no tenant, no message id or no valid sender phone.
func WhatsAppMessage(e WhatsAppInbound, autoTicket bool) (Message, bool) {
	tenantID, err := uuid.Parse(strings.TrimSpace(e.TenantID))
	if err != nil || tenantID == uuid.Nil || strings.TrimSpace(e.WAMessageID) == "" {
		return Message{}, false
	}
	phone, err := contact.NormalizePhone(e.From, "")
	if err != nil {
		return Message{}, false
	}
	text := strings.TrimSpace(e.Text)
	kind := strings.ToLower(strings.TrimSpace(e.Type))
	if kind != "" && kind != "text" && kind != "button" && kind != "interactive" {
		// Media arrives as a reference notifications-api keeps; the ticket shows what it was.
		label := "[WhatsApp " + kind + " message]"
		if text == "" {
			text = label
		} else {
			text = label + "\n" + text
		}
	}
	m := Message{
		TenantID:      tenantID,
		Channel:       tickets.ChannelWhatsApp,
		ExternalID:    strings.TrimSpace(e.WAMessageID),
		Requester:     tickets.RequesterContact{Name: strings.TrimSpace(e.ContactName), Phone: phone},
		Text:          text,
		ThreadOnly:    !autoTicket,
		Extra:         map[string]any{"phone_number_id": e.PhoneNumberID, "wa_type": kind},
		PhoneNumberID: strings.TrimSpace(e.PhoneNumberID),
		ReceivedAt:    parseTime(e.ReceivedAt),
	}
	if e.ReplyID != "" {
		m.Extra["reply_to_wa_message_id"] = e.ReplyID
	}
	if stopWords[strings.ToLower(strings.Join(strings.Fields(text), " "))] {
		m.Refusal = "opt-out reply"
	}
	return m, true
}
