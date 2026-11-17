package tickets

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var windowStart = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

// A ticket opened from a WhatsApp message records the window: when the message came, its id and
// the tenant number it came in on.
func TestWhatsAppCreateRecordsWindow(t *testing.T) {
	repo := newInboundRepo()
	s := NewService(repo, zap.NewNop())
	s.now = func() time.Time { return windowStart }
	tk, err := s.CreateTicket(context.Background(), &CreateTicketRequest{
		Subject: "Order late", Description: "Where is it", Channel: ChannelWhatsApp, RequesterPhone: "+254712345678",
		Inbound: &InboundOrigin{Key: MessageKey{Channel: ChannelWhatsApp, ID: "wamid.1"},
			PhoneNumberID: "pn-1", ReceivedAt: windowStart.Add(-time.Minute)},
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := repo.tickets[tk.ID].Metadata
	if meta[MetaWhatsAppLastInboundAt] != "2026-11-17T08:59:00Z" || meta[MetaWhatsAppLastMessageID] != "wamid.1" ||
		meta[MetaWhatsAppPhoneNumberID] != "pn-1" {
		t.Fatalf("metadata = %v", meta)
	}
	if tk.WhatsAppWindowExpiresAt == nil || !tk.WhatsAppWindowExpiresAt.Equal(windowStart.Add(-time.Minute).Add(WhatsAppWindow)) {
		t.Fatalf("expires = %v", tk.WhatsAppWindowExpiresAt)
	}
}

// A later message moves the window forward; a late delivery of an older one never moves it back,
// and a received time ahead of our clock counts as now.
func TestWhatsAppThreadMovesWindowForwardOnly(t *testing.T) {
	repo := newInboundRepo()
	s := NewService(repo, zap.NewNop())
	s.now = func() time.Time { return windowStart }
	tk := repo.addTicket("TKT-000009", StatusOpen)
	tk.Channel = ChannelWhatsApp
	tk.Metadata[MetaWhatsAppLastInboundAt] = windowStart.Add(-2 * time.Hour).Format(time.RFC3339)
	tk.Metadata[MetaWhatsAppLastMessageID] = "wamid.old"

	add := func(id string, at time.Time) {
		t.Helper()
		_, err := s.AddInboundMessage(context.Background(), InboundMessage{
			TenantID: tk.TenantID, TicketID: tk.ID, Key: MessageKey{Channel: ChannelWhatsApp, ID: id},
			From: RequesterContact{Phone: "+254712345678"}, Content: "hi", PhoneNumberID: "pn-1", ReceivedAt: at,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add("wamid.new", windowStart.Add(-time.Hour))
	if tk.Metadata[MetaWhatsAppLastMessageID] != "wamid.new" || tk.Metadata[MetaWhatsAppPhoneNumberID] != "pn-1" {
		t.Fatalf("after a newer message: %v", tk.Metadata)
	}
	add("wamid.late", windowStart.Add(-3*time.Hour))
	if tk.Metadata[MetaWhatsAppLastMessageID] != "wamid.new" {
		t.Fatalf("a late delivery moved the window back: %v", tk.Metadata)
	}
	add("wamid.future", windowStart.Add(time.Hour))
	if tk.Metadata[MetaWhatsAppLastInboundAt] != windowStart.Format(time.RFC3339) || tk.Metadata[MetaWhatsAppLastMessageID] != "wamid.future" {
		t.Fatalf("a future time must count as now: %v", tk.Metadata)
	}
}

func TestWhatsAppWindowExpiresAt(t *testing.T) {
	last := windowStart.Format(time.RFC3339)
	cases := []struct {
		name string
		t    *Ticket
		want *time.Time
	}{
		{"email ticket", &Ticket{Channel: ChannelEmail, Metadata: map[string]any{MetaWhatsAppLastInboundAt: last}}, nil},
		{"no inbound yet", &Ticket{Channel: ChannelWhatsApp, Metadata: map[string]any{}}, nil},
		{"bad time", &Ticket{Channel: ChannelWhatsApp, Metadata: map[string]any{MetaWhatsAppLastInboundAt: "yesterday"}}, nil},
	}
	for _, c := range cases {
		if got := WhatsAppWindowExpiresAt(c.t); got != nil {
			t.Errorf("%s: got %v, want nil", c.name, got)
		}
	}
	got := WhatsAppWindowExpiresAt(&Ticket{Channel: ChannelWhatsApp, Metadata: map[string]any{MetaWhatsAppLastInboundAt: last}})
	if got == nil || !got.Equal(windowStart.Add(24*time.Hour)) {
		t.Fatalf("got %v", got)
	}
}

// The DTO always carries whatsapp_window_expires_at: null when there is no window.
func TestTicketJSONWindowField(t *testing.T) {
	raw, err := json.Marshal(&Ticket{Channel: ChannelEmail})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"whatsapp_window_expires_at":null`) {
		t.Fatalf("json = %s", raw)
	}
}

func whatsAppTicket(lastInbound time.Time) *Ticket {
	return &Ticket{ID: uuid.New(), TicketNumber: "TKT-000010", Channel: ChannelWhatsApp, Metadata: map[string]any{
		MetaRequesterPhone:        "+254712345678",
		MetaWhatsAppLastInboundAt: lastInbound.Format(time.RFC3339),
		MetaWhatsAppLastMessageID: "wamid.7",
		MetaWhatsAppPhoneNumberID: "pn-1",
	}}
}

// An agent reply on a WhatsApp ticket says whether the window is open, which message it answers,
// the tenant number and the whole reply as WhatsApp text.
func TestCommentAddedWhatsAppFields(t *testing.T) {
	c := &TicketComment{ID: uuid.New(), AuthorType: AuthorAgent, Content: "<p>Sent again.</p><ul><li>Check M-Pesa</li></ul>"}

	open := TicketCommentAddedPayload(whatsAppTicket(windowStart.Add(-23*time.Hour)), c, "", windowStart)
	if open["whatsapp_window_open"] != true || open["whatsapp_reply_to"] != "wamid.7" || open["phone_number_id"] != "pn-1" ||
		open["requester_phone"] != "+254712345678" || open["whatsapp_window_expires_at"] != "2026-11-17T10:00:00Z" {
		t.Fatalf("open window payload = %v", open)
	}
	text, _ := open["whatsapp_text"].(string)
	if !strings.Contains(text, "Sent again.") || !strings.Contains(text, "\n") || strings.Contains(text, "<") {
		t.Fatalf("whatsapp_text = %q", text)
	}

	closed := TicketCommentAddedPayload(whatsAppTicket(windowStart.Add(-25*time.Hour)), c, "", windowStart)
	if closed["whatsapp_window_open"] != false || closed["whatsapp_reply_to"] != "wamid.7" {
		t.Fatalf("closed window payload = %v", closed)
	}

	// Another channel, or no requester phone: the keys are there and empty.
	email := whatsAppTicket(windowStart)
	email.Channel = ChannelEmail
	noPhone := whatsAppTicket(windowStart)
	delete(noPhone.Metadata, MetaRequesterPhone)
	for _, tk := range []*Ticket{email, noPhone} {
		p := TicketCommentAddedPayload(tk, c, "", windowStart)
		if p["whatsapp_window_open"] != false || p["whatsapp_reply_to"] != "" || p["phone_number_id"] != "" || p["whatsapp_text"] != "" {
			t.Fatalf("payload = %v", p)
		}
	}
}

func TestWhatsAppTextLimit(t *testing.T) {
	got := whatsAppText(strings.Repeat("a", whatsAppTextLimit+50))
	if n := len([]rune(got)); n != whatsAppTextLimit || !strings.HasSuffix(got, "...") {
		t.Fatalf("len %d, suffix %q", n, got[len(got)-3:])
	}
}
