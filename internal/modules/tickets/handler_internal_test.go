package tickets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// The S2S routes answer only with the service key and an X-Tenant-ID uuid, and refuse a bad body
// before anything is created.
func TestInternalRoutesNeedTheKeyAndTenant(t *testing.T) {
	h := &Handler{serviceKey: "right-key"}
	create := httptest.NewRequest(http.MethodPost, "/internal/tickets", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.InternalCreateTicket(rec, create)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("create without a key: %d", rec.Code)
	}
	read := httptest.NewRequest(http.MethodGet, "/internal/tickets/T-1", nil)
	read.Header.Set("X-Tenant-ID", uuid.NewString())
	rec = httptest.NewRecorder()
	h.InternalGetTicket(rec, read)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("read without a key: %d", rec.Code)
	}
	ok := `"subject":"S","description":"D","source":"projects.qa"`
	for name, c := range map[string]struct{ tenant, body string }{
		"no tenant header":  {"", `{` + ok + `}`},
		"tenant not a uuid": {"acme", `{` + ok + `}`},
		"priority":          {uuid.NewString(), `{` + ok + `,"priority":"highest"}`},
		"channel":           {uuid.NewString(), `{` + ok + `,"channel":"pigeon"}`},
		"no subject":        {uuid.NewString(), `{"description":"D","source":"projects.qa"}`},
		"title is not read": {uuid.NewString(), `{"title":"T","description":"D","source":"projects.qa"}`},
		"no source":         {uuid.NewString(), `{"subject":"S","description":"D"}`},
		"ref without id":    {uuid.NewString(), `{` + ok + `,"source_ref":{"type":"qa_defect"}}`},
		"parent without id": {uuid.NewString(), `{` + ok + `,"source_ref":{"type":"qa_defect","id":"1","parent":{"type":"qa_session"}}}`},
	} {
		req := httptest.NewRequest(http.MethodPost, "/internal/tickets", strings.NewReader(c.body))
		req.Header.Set("X-API-Key", "right-key")
		if c.tenant != "" {
			req.Header.Set("X-Tenant-ID", c.tenant)
		}
		rec := httptest.NewRecorder()
		h.InternalCreateTicket(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

// sourceRepo is a fake keyed by (tenant, source, ref type, ref id) that refuses a second insert for
// the same key, as the ticket_tenant_source_ref unique index does.
type sourceRepo struct {
	fakeRepo
	mu      sync.Mutex
	bySrc   map[string]*Ticket
	numbers map[uuid.UUID]int64
}

func newSourceRepo() *sourceRepo {
	return &sourceRepo{fakeRepo: *newFakeRepo(), bySrc: map[string]*Ticket{}, numbers: map[uuid.UUID]int64{}}
}

func srcKey(tenant uuid.UUID, meta map[string]any) string {
	ref, _ := meta["source_ref"].(map[string]any)
	return tenant.String() + "|" + metaText(meta, "source") + "|" + metaText(ref, "type") + "|" + metaText(ref, "id")
}

func (r *sourceRepo) InTx(ctx context.Context, tenant uuid.UUID, fn func(Repository, *Outbox) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(r, &Outbox{})
}

func (r *sourceRepo) NextTicketNumber(_ context.Context, tenant uuid.UUID) (string, error) {
	r.numbers[tenant]++
	return FormatTicketNumber(r.numbers[tenant]), nil
}

func (r *sourceRepo) CreateTicket(_ context.Context, tenant uuid.UUID, t *Ticket) error {
	if _, ok := t.Metadata["source_ref"]; ok {
		k := srcKey(tenant, t.Metadata)
		if _, dup := r.bySrc[k]; dup {
			return ErrDuplicateSourceRef
		}
		cp := *t
		r.bySrc[k] = &cp
	}
	t.CreatedAt = time.Now()
	return nil
}

func (r *sourceRepo) FindTicketBySourceRef(_ context.Context, tenant uuid.UUID, source string, ref SourceRef) (*Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := srcKey(tenant, map[string]any{"source": source, "source_ref": map[string]any{"type": ref.Type, "id": ref.ID}})
	if t, ok := r.bySrc[k]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, ErrTicketNotFound
}

func internalCreate(h *Handler, tenant uuid.UUID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/tickets", strings.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	req.Header.Set("X-Tenant-ID", tenant.String())
	rec := httptest.NewRecorder()
	h.InternalCreateTicket(rec, req)
	return rec
}

// One QA defect makes one ticket: a repeat answers 200 with the same ticket. Another defect of the
// same session (same parent) is its own ticket.
func TestInternalCreateIsIdempotentOnSourceRef(t *testing.T) {
	repo := newSourceRepo()
	h := NewHandler(NewService(repo, zap.NewNop()), zap.NewNop(), WithServiceKey("k"))
	tenant := uuid.New()
	session := uuid.NewString()
	body := func(defect string) string {
		return `{"subject":"QA failed","description":"Step 3","priority":"high","source":"projects.qa",` +
			`"source_ref":{"type":"qa_defect","id":"` + defect + `","parent":{"type":"qa_session","id":"` + session + `"}}}`
	}
	defect := uuid.NewString()
	first := internalCreate(h, tenant, body(defect))
	if first.Code != http.StatusCreated {
		t.Fatalf("first create: %d %s", first.Code, first.Body)
	}
	again := internalCreate(h, tenant, body(defect))
	if again.Code != http.StatusOK {
		t.Fatalf("repeat create: %d %s", again.Code, again.Body)
	}
	var a, b map[string]any
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(again.Body.Bytes(), &b)
	if a["id"] != b["id"] || a["ticket_number"] != b["ticket_number"] || a["status"] != "new" {
		t.Fatalf("repeat returned another ticket: %v vs %v", a, b)
	}
	other := internalCreate(h, tenant, body(uuid.NewString()))
	if other.Code != http.StatusCreated {
		t.Fatalf("second defect of the session: %d", other.Code)
	}
	stored := repo.bySrc[srcKey(tenant, map[string]any{"source": "projects.qa",
		"source_ref": map[string]any{"type": "qa_defect", "id": defect}})]
	ref, _ := stored.Metadata["source_ref"].(map[string]any)
	parent, _ := ref["parent"].(map[string]any)
	if parent["id"] != session || stored.Priority != PriorityHigh {
		t.Fatalf("stored metadata: %v", stored.Metadata)
	}
}

// A create that loses the race to the unique index answers with the winner, not an error.
func TestCreateInternalTicketResolvesTheRace(t *testing.T) {
	repo := newSourceRepo()
	s := NewService(repo, zap.NewNop())
	tenant := uuid.New()
	ref := &SourceRef{Type: "qa_defect", ID: "d-1"}
	var wg sync.WaitGroup
	results := make([]*Ticket, 8)
	created := make([]bool, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := &CreateTicketRequest{TenantID: tenant, Subject: "S", Description: "D"}
			tk, c, err := s.CreateInternalTicket(context.Background(), req, "projects.qa", ref)
			if err != nil {
				t.Errorf("create %d: %v", i, err)
				return
			}
			results[i], created[i] = tk, c
		}(i)
	}
	wg.Wait()
	n := 0
	for i, c := range created {
		if c {
			n++
		}
		if results[i] == nil || results[i].TicketNumber != results[0].TicketNumber {
			t.Fatalf("calls disagree on the ticket: %v", results)
		}
	}
	if n != 1 {
		t.Fatalf("%d creates reported a new ticket, want 1", n)
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
		{AuthorType: AuthorAgent, CommentType: CommentInternal, Content: "secret note", CreatedAt: now},
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

// waRepo adds the comment lookup the WhatsApp conversion reaches: externalID to the ticket the
// channel ingest threaded that message onto.
type waRepo struct {
	*sourceRepo
	threaded map[string]*Ticket
}

func (r *waRepo) FindCommentByExternalID(_ context.Context, _ uuid.UUID, channel, externalID string) (*TicketComment, error) {
	if t, ok := r.threaded[channel+"|"+externalID]; ok {
		return &TicketComment{ID: uuid.New(), TicketID: t.ID}, nil
	}
	return nil, ErrCommentNotFound
}

func (r *waRepo) GetTicket(_ context.Context, _ uuid.UUID, id uuid.UUID) (*Ticket, error) {
	for _, t := range r.threaded {
		if t.ID == id {
			cp := *t
			return &cp, nil
		}
	}
	return nil, ErrTicketNotFound
}

// Convert to ticket from the notifications inbox uses the channel ingest's key: a repeat answers
// with the same ticket, the ticket carries the WhatsApp origin and reply window, and a message the
// ingest already threaded onto a ticket answers with that ticket.
func TestInternalCreateConvertsAWhatsAppMessageOnce(t *testing.T) {
	repo := &waRepo{sourceRepo: newSourceRepo(), threaded: map[string]*Ticket{}}
	h := NewHandler(NewService(repo, zap.NewNop()), zap.NewNop(), WithServiceKey("k"))
	tenant := uuid.New()
	body := func(id string) string {
		return `{"subject":"My order","description":"My order has not arrived","channel":"whatsapp",` +
			`"source":"notifications.whatsapp","source_ref":{"type":"wa_message","id":"` + id + `"},` +
			`"requester_name":"Amina","requester_phone":"+254712345678",` +
			`"phone_number_id":"pn-1","received_at":"2025-01-06T08:00:00Z"}`
	}
	first := internalCreate(h, tenant, body("wamid.1"))
	if first.Code != http.StatusCreated {
		t.Fatalf("convert: %d %s", first.Code, first.Body)
	}
	again := internalCreate(h, tenant, body("wamid.1"))
	if again.Code != http.StatusOK {
		t.Fatalf("second convert: %d %s", again.Code, again.Body)
	}
	var a, b map[string]any
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(again.Body.Bytes(), &b)
	if a["id"] != b["id"] {
		t.Fatalf("second convert opened another ticket: %v vs %v", a, b)
	}
	key := MessageKey{Channel: ChannelWhatsApp, ID: "wamid.1"}
	stored := repo.bySrc[srcKey(tenant, map[string]any{"source": key.Source(),
		"source_ref": map[string]any{"type": key.Ref().Type, "id": key.ID}})]
	if stored == nil {
		t.Fatal("ticket not stored under the ingest key")
	}
	if stored.Channel != ChannelWhatsApp || stored.Metadata[MetaWhatsAppLastMessageID] != "wamid.1" ||
		stored.Metadata[MetaWhatsAppPhoneNumberID] != "pn-1" || stored.Metadata[MetaWhatsAppLastInboundAt] != "2025-01-06T08:00:00Z" {
		t.Fatalf("whatsapp origin missing: %v", stored.Metadata)
	}
	if stored.CustomerID != CustomerIDFor(RequesterContact{Phone: "+254712345678"}) {
		t.Fatalf("customer id differs from the ingest's: %s", stored.CustomerID)
	}

	open := &Ticket{ID: uuid.New(), TicketNumber: "TKT-000042", Status: StatusOpen}
	repo.threaded["whatsapp|wamid.2"] = open
	threaded := internalCreate(h, tenant, body("wamid.2"))
	var c map[string]any
	_ = json.Unmarshal(threaded.Body.Bytes(), &c)
	if threaded.Code != http.StatusOK || c["id"] != open.ID.String() || c["ticket_number"] != "TKT-000042" {
		t.Fatalf("threaded message: %d %v", threaded.Code, c)
	}
}

// Only the ingest's own key is a channel message; another source or ref type is a plain create.
func TestChannelMessageKey(t *testing.T) {
	if _, ok := channelMessageKey("notifications.whatsapp", &SourceRef{Type: "wa_message", ID: "x"}); !ok {
		t.Fatal("whatsapp key not recognised")
	}
	for _, c := range []struct {
		source string
		ref    *SourceRef
	}{
		{"notifications.whatsapp", nil},
		{"notifications.whatsapp", &SourceRef{Type: "conversation", ID: "x"}},
		{"projects.qa", &SourceRef{Type: "wa_message", ID: "x"}},
	} {
		if _, ok := channelMessageKey(c.source, c.ref); ok {
			t.Errorf("%s %+v treated as a channel message", c.source, c.ref)
		}
	}
}
