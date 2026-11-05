package tickets

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The S2S routes answer only with the service key, and refuse a bad priority or channel before anything
// is created. No key means 401 on both.
func TestInternalRoutesNeedTheKey(t *testing.T) {
	h := &Handler{serviceKey: "right-key"}
	create := httptest.NewRequest(http.MethodPost, "/internal/tickets", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.InternalCreateTicket(rec, create)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("create without a key: %d", rec.Code)
	}
	read := httptest.NewRequest(http.MethodGet, "/internal/tickets/T-1?tenant_id="+uuid.NewString(), nil)
	rec = httptest.NewRecorder()
	h.InternalGetTicket(rec, read)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("read without a key: %d", rec.Code)
	}
	for name, body := range map[string]string{
		"priority": `{"tenant_id":"` + uuid.NewString() + `","title":"T","description":"D","priority":"highest"}`,
		"channel":  `{"tenant_id":"` + uuid.NewString() + `","title":"T","description":"D","channel":"pigeon"}`,
		"tenant":   `{"tenant_id":"nope","title":"T","description":"D"}`,
		"title":    `{"tenant_id":"` + uuid.NewString() + `","description":"D"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/internal/tickets", strings.NewReader(body))
		req.Header.Set("X-API-Key", "right-key")
		rec := httptest.NewRecorder()
		h.InternalCreateTicket(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

// A calling service gets the ticket and public comments only: no internal note, no contact details.
func TestInternalTicketView(t *testing.T) {
	now := time.Now()
	ticket := &Ticket{ID: uuid.New(), TicketNumber: "T-9", Status: "open", Priority: PriorityCritical, Channel: ChannelWeb, Subject: "S",
		Description: "D", Metadata: map[string]any{"category": "payout", "contact_hash": "abc", "requester_phone": "+254700000000",
			"requester_email": "guest@example.com"}, CreatedAt: now, UpdatedAt: now}
	comments := []*TicketComment{
		{AuthorType: AuthorAgent, CommentType: CommentPublic, Content: "We are on it", CreatedAt: now},
		{AuthorType: AuthorAgent, CommentType: CommentType("internal"), Content: "secret note", CreatedAt: now},
	}
	b, err := json.Marshal(internalTicketView(ticket, comments))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "secret note") || strings.Contains(text, "+254700000000") || strings.Contains(text, "guest@example.com") {
		t.Fatalf("leaked: %s", text)
	}
	var out struct {
		TicketNumber string `json:"ticket_number"`
		Category     string `json:"category"`
		ContactHash  string `json:"contact_hash"`
		Comments     []struct {
			Content string `json:"content"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.TicketNumber != "T-9" || out.Category != "payout" || out.ContactHash != "abc" || len(out.Comments) != 1 || out.Comments[0].Content != "We are on it" {
		t.Fatalf("view = %+v", out)
	}
}
