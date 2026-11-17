package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/Bengo-Hub/httpware/contact"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// EmailInbound is the email.inbound.support event email-provisioner publishes once per message
// it reads from the support mailboxes. The flags after attachments are optional loop and spam
// signals; Headers holds any other headers it passes, keyed in lower case.
type EmailInbound struct {
	MessageID   string            `json:"message_id"`
	InReplyTo   string            `json:"in_reply_to"`
	References  []string          `json:"references"`
	FromEmail   string            `json:"from_email"`
	FromName    string            `json:"from_name"`
	To          []string          `json:"to"`
	Cc          []string          `json:"cc"`
	Recipient   string            `json:"recipient"`
	Subject     string            `json:"subject"`
	Text        string            `json:"text"`
	HTML        string            `json:"html"`
	ReceivedAt  string            `json:"received_at"`
	Attachments []EmailAttachment `json:"attachments"`

	AutoSubmitted string            `json:"auto_submitted"`
	Spam          bool              `json:"spam"`
	Virus         bool              `json:"virus"`
	Headers       map[string]string `json:"headers"`
}

// EmailAttachment is a file the reader already stored in the private bucket.
type EmailAttachment struct {
	Key         string `json:"key"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	ScanStatus  string `json:"scan_status"`
}

// maxAttachments bounds the files one email adds.
const maxAttachments = 20

// decodePayload reads an event body that is either the bare payload or a shared-events envelope
// carrying it under "payload". The envelope's tenant_id is returned when it has one.
func decodePayload(data []byte, into any) (envelopeTenant string, err error) {
	var env struct {
		TenantID string          `json:"tenant_id"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err == nil && len(env.Payload) > 0 && env.Payload[0] == '{' {
		return env.TenantID, json.Unmarshal(env.Payload, into)
	}
	return "", json.Unmarshal(data, into)
}

// EmailMessage turns the event into a Message for the tenant. ok is false when the sender has no
// usable address.
func EmailMessage(e EmailInbound, tenantID uuid.UUID, slug string, addrs Addresses) (Message, bool) {
	from, err := contact.NormalizeEmail(e.FromEmail)
	if err != nil {
		return Message{}, false
	}
	// In-Reply-To first, then References newest first (the header lists them oldest first).
	refs := ParseMessageIDs(append(ParseMessageIDs(e.InReplyTo), reversed(ParseMessageIDs(e.References...))...)...)
	m := Message{
		TenantID:     tenantID,
		Channel:      tickets.ChannelEmail,
		ExternalID:   emailExternalID(e, from),
		ThreadRefs:   refs,
		TicketNumber: SubjectTicketNumber(e.Subject),
		Requester:    tickets.RequesterContact{Name: strings.TrimSpace(e.FromName), Email: from},
		Subject:      e.Subject,
		Text:         e.Text,
		HTML:         e.HTML,
		Attachments:  emailFiles(e.Attachments, attachmentOwner(slug, addrs)),
		Refusal:      emailRefusal(e, from, addrs),
		Extra:        map[string]any{"from_email": from, "recipient": strings.ToLower(strings.TrimSpace(e.Recipient))},
		ReceivedAt:   parseTime(e.ReceivedAt),
	}
	if cc := cleanAddresses(e.Cc, addrs); len(cc) > 0 {
		m.Extra["cc"] = cc
	}
	return m, true
}

// emailExternalID is the Message-ID without brackets, or, for the rare mail without one, a hash
// of what identifies it, so a redelivery still dedupes.
func emailExternalID(e EmailInbound, from string) string {
	if ids := ParseMessageIDs(e.MessageID); len(ids) > 0 {
		return ids[0]
	}
	sum := sha256.Sum256([]byte(from + "\n" + e.Subject + "\n" + e.ReceivedAt + "\n" + e.Text))
	return "sha256:" + hex.EncodeToString(sum[:16])
}

var (
	noReplyLocal = regexp.MustCompile(`^(no-?reply|do-?not-?reply|donotreply|mailer-daemon|postmaster|bounces?)([+.\-_].*)?$`)
	autoSubject  = regexp.MustCompile(`(?i)^\s*(auto(matic)?[ -]?(reply|response)|auto:|out of (the )?office|undeliver(able|ed)|delivery status notification|mail delivery (failed|failure|subsystem)|returned mail|failure notice)`)
)

// emailRefusal says why an email must not open or continue a ticket, or "" when it may: our own
// mail, no-reply and daemon senders, auto-replies (RFC 3834 Auto-Submitted, X-Autoreply,
// Precedence), bounce reports, and mail flagged as spam or carrying a virus.
func emailRefusal(e EmailInbound, from string, addrs Addresses) string {
	h := func(name string) string { return strings.ToLower(strings.TrimSpace(e.Headers[name])) }
	local, _, _ := strings.Cut(from, "@")
	auto := strings.ToLower(strings.TrimSpace(e.AutoSubmitted))
	if auto == "" {
		auto = h("auto-submitted")
	}
	switch {
	case addrs.IsOwn(from):
		return "sent from one of our own addresses"
	case noReplyLocal.MatchString(local):
		return "no-reply sender"
	case auto != "" && auto != "no":
		return "auto-submitted"
	case h("x-autoreply") != "" || h("x-autorespond") != "" || h("x-auto-response-suppress") == "all":
		return "auto-reply"
	case h("precedence") == "bulk" || h("precedence") == "junk" || h("precedence") == "auto_reply" || h("precedence") == "list":
		return "bulk or list mail"
	case strings.Contains(h("content-type"), "report-type=delivery-status"):
		return "delivery status report"
	case autoSubject.MatchString(e.Subject):
		return "auto-reply subject"
	case e.Virus:
		return "virus found"
	case e.Spam:
		return "flagged as spam"
	}
	return ""
}

// attachmentOwner is the owner segment email-provisioner uses in an inbound attachment key: the
// tenant's slug, or "platform" for the platform tenant's own support mailbox.
func attachmentOwner(slug string, addrs Addresses) string {
	if slug == addrs.PlatformSlug {
		return "platform"
	}
	return slug
}

// emailFiles keeps the attachments that are safe to record: a key in the tenant's own inbound
// mail prefix, not reported infected.
func emailFiles(in []EmailAttachment, owner string) []tickets.InboundFile {
	var out []tickets.InboundFile
	for _, a := range in {
		if len(out) == maxAttachments {
			break
		}
		if !tenantAttachmentKey(a.Key, owner) || strings.EqualFold(a.ScanStatus, "infected") {
			continue
		}
		out = append(out, tickets.InboundFile{
			Key: a.Key, Filename: a.Filename, ContentType: a.ContentType, Size: a.Size, ScanStatus: a.ScanStatus,
		})
	}
	return out
}

var keySegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// inboundKeyService and inboundKeyMediaType are the fixed segments of the keys email-provisioner
// writes for inbound support mail: support-inbound/<owner>/inbound-mail/<message hash>/<file>.
const (
	inboundKeyService   = "support-inbound"
	inboundKeyMediaType = "inbound-mail"
)

// tenantAttachmentKey checks an object key has email-provisioner's inbound layout and belongs to
// the tenant, so an event can never attach another tenant's file.
func tenantAttachmentKey(key, owner string) bool {
	segs := strings.Split(key, "/")
	if owner == "" || len(segs) < 5 {
		return false
	}
	for _, s := range segs {
		if !keySegment.MatchString(s) || strings.Contains(s, "..") {
			return false
		}
	}
	return segs[0] == inboundKeyService && segs[1] == owner && segs[2] == inboundKeyMediaType
}

// cleanAddresses normalises addresses and leaves out ours.
func cleanAddresses(in []string, addrs Addresses) []string {
	var out []string
	for _, a := range in {
		if e, err := contact.NormalizeEmail(a); err == nil && !addrs.IsOwn(e) {
			out = append(out, e)
		}
	}
	return out
}

func reversed(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
