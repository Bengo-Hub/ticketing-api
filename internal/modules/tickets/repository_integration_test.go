//go:build integration

package tickets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Per-tenant numbering holds under 50 concurrent creates over two tenants: each tenant gets
// TKT-000001 to TKT-000025 with no gap and no repeat, and its counter ends at 25. The creates go
// through Service.CreateTicket, so the number, the ticket and the outbox rows share one
// transaction as they do in production.
func TestIntegrationTicketNumbersUnderConcurrency(t *testing.T) {
	env := newITEnv(t)
	ctx := context.Background()
	tenants := []uuid.UUID{uuid.New(), uuid.New()}
	const perTenant = 25

	start := make(chan struct{})
	errs := make(chan error, perTenant*len(tenants))
	var wg sync.WaitGroup
	for i := 0; i < perTenant; i++ {
		for _, tenant := range tenants {
			wg.Add(1)
			go func(tenant uuid.UUID, i int) {
				defer wg.Done()
				<-start
				_, err := env.svc.CreateTicket(ctx, &CreateTicketRequest{
					TenantID: tenant, CustomerID: uuid.New(),
					Subject: fmt.Sprintf("Concurrent create %d", i), Description: "Load",
				})
				if err != nil {
					errs <- err
				}
			}(tenant, i)
		}
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("create: %v", err)
	}

	for _, tenant := range tenants {
		rows, err := env.db.SQL.QueryContext(ctx,
			`SELECT ticket_number FROM tickets WHERE tenant_id = $1 ORDER BY ticket_number`, tenant)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			got = append(got, n)
		}
		rows.Close()
		if len(got) != perTenant {
			t.Fatalf("tenant %s has %d tickets, want %d", tenant, len(got), perTenant)
		}
		for i, n := range got {
			if want := FormatTicketNumber(int64(i + 1)); n != want {
				t.Errorf("tenant %s ticket %d is %s, want %s (gap or repeat)", tenant, i, n, want)
			}
		}
		if next := env.db.Int64(t, `SELECT next_value FROM ticket_sequences WHERE tenant_id = $1`, tenant); next != perTenant {
			t.Errorf("tenant %s counter is %d, want %d", tenant, next, perTenant)
		}
		// Every create wrote its ticket.created event in the same transaction.
		if n := env.db.Int64(t, `SELECT count(*) FROM outbox_events WHERE tenant_id = $1`, tenant); n < perTenant {
			t.Errorf("tenant %s has %d outbox rows, want at least %d", tenant, n, perTenant)
		}
	}
}

// itSorted orders tickets as the created sorts do: (created_at, id), uuid compared bytewise as
// Postgres compares it.
func itSorted(ts []*Ticket, asc bool) []uuid.UUID {
	cp := append([]*Ticket(nil), ts...)
	sort.Slice(cp, func(i, j int) bool {
		a, b := cp[i], cp[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			if asc {
				return a.CreatedAt.Before(b.CreatedAt)
			}
			return a.CreatedAt.After(b.CreatedAt)
		}
		c := bytes.Compare(a.ID[:], b.ID[:])
		if asc {
			return c < 0
		}
		return c > 0
	})
	out := make([]uuid.UUID, len(cp))
	for i, tk := range cp {
		out[i] = tk.ID
	}
	return out
}

// itPageAll walks a list to its end with the cursors the service hands out.
func itPageAll(t *testing.T, env *itEnv, tenant uuid.UUID, f TicketFilter, limit int) []uuid.UUID {
	t.Helper()
	f.Page = PageParams{Limit: limit}
	var ids []uuid.UUID
	for i := 0; i < 100; i++ {
		page, err := env.svc.ListTickets(context.Background(), tenant, f)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(page.Data) > limit {
			t.Fatalf("page of %d rows, limit %d", len(page.Data), limit)
		}
		for _, tk := range page.Data {
			ids = append(ids, tk.ID)
		}
		if !page.HasMore {
			return ids
		}
		c, err := ParseCursor(page.NextCursor)
		if err != nil {
			t.Fatalf("cursor %q: %v", page.NextCursor, err)
		}
		f.Page.After = c
	}
	t.Fatal("paging never ended")
	return nil
}

func itSameOrder(t *testing.T, label string, got, want []uuid.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d rows, want %d", label, len(got), len(want))
	}
	seen := map[uuid.UUID]bool{}
	for i := range got {
		if seen[got[i]] {
			t.Fatalf("%s: %s appears twice", label, got[i])
		}
		seen[got[i]] = true
		if got[i] != want[i] {
			t.Fatalf("%s: row %d is %s, want %s", label, i, got[i], want[i])
		}
	}
}

// Keyset paging returns every live ticket of the tenant once, in order, across pages, including
// rows that share a created_at (the id breaks the tie), in both created orders and under
// sort=sla_due.
func TestIntegrationListTicketsKeysetPaging(t *testing.T) {
	env := newITEnv(t)
	tenant, other := uuid.New(), uuid.New()
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	offsets := []time.Duration{0, time.Minute, 2 * time.Minute, 2 * time.Minute, 2 * time.Minute, 5 * time.Minute, 7 * time.Minute, 9 * time.Minute}
	var live []*Ticket
	for i, off := range offsets {
		tk := env.insert(t, tenant, func(tk *Ticket) {
			// A mix of SLA states for sort=sla_due: due, no target, paused.
			switch i % 3 {
			case 0:
				tk.SLAResponseDueAt = itPtr(base.Add(time.Duration(10-i) * time.Hour))
			case 1:
				tk.SLAResolutionDueAt = itPtr(base.Add(time.Duration(i) * time.Hour))
			}
		})
		created := base.Add(off)
		env.db.Exec(t, `UPDATE tickets SET created_at = $2 WHERE id = $1`, tk.ID, created)
		if i == 4 {
			env.db.Exec(t, `UPDATE tickets SET sla_paused_at = $2 WHERE id = $1`, tk.ID, base)
		}
		tk.CreatedAt = created
		live = append(live, tk)
	}
	deleted := env.insert(t, tenant, nil)
	if err := env.repo.DeleteTicket(context.Background(), tenant, deleted.ID); err != nil {
		t.Fatal(err)
	}
	env.insert(t, other, nil)

	itSameOrder(t, "created_desc", itPageAll(t, env, tenant, TicketFilter{}, 3), itSorted(live, false))
	itSameOrder(t, "created_asc", itPageAll(t, env, tenant, TicketFilter{Sort: SortCreatedAsc}, 3), itSorted(live, true))
	itSameOrder(t, "created_desc limit 1", itPageAll(t, env, tenant, TicketFilter{}, 1), itSorted(live, false))

	// sort=sla_due: the pages put together equal one page holding everything.
	whole := itPageAll(t, env, tenant, TicketFilter{Sort: SortSLADue}, 100)
	itSameOrder(t, "sla_due paged", itPageAll(t, env, tenant, TicketFilter{Sort: SortSLADue}, 2), whole)
	if len(whole) != len(live) {
		t.Fatalf("sla_due lists %d tickets, want %d", len(whole), len(live))
	}
}

func itIDSet(ts []*Ticket) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, tk := range ts {
		out[tk.ID] = true
	}
	return out
}

func itExpect(t *testing.T, env *itEnv, tenant uuid.UUID, label string, f TicketFilter, want ...*Ticket) {
	t.Helper()
	f.Page = PageParams{Limit: 100}
	page, err := env.svc.ListTickets(context.Background(), tenant, f)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	got, exp := itIDSet(page.Data), itIDSet(want)
	if len(got) != len(exp) {
		t.Errorf("%s: %d tickets, want %d", label, len(got), len(exp))
	}
	for id := range exp {
		if !got[id] {
			t.Errorf("%s: missing %s", label, id)
		}
	}
	for id := range got {
		if !exp[id] {
			t.Errorf("%s: unexpected %s", label, id)
		}
	}
}

func TestIntegrationListTicketsFilters(t *testing.T) {
	env := newITEnv(t)
	tenant, other := uuid.New(), uuid.New()
	teamX, teamY := uuid.New(), uuid.New()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	mk := func(created time.Time, mut func(*Ticket)) *Ticket {
		tk := env.insert(t, tenant, mut)
		env.db.Exec(t, `UPDATE tickets SET created_at = $2 WHERE id = $1`, tk.ID, created)
		return tk
	}
	t1 := mk(day(1), func(tk *Ticket) {
		tk.Status, tk.Priority, tk.Channel, tk.Tags, tk.AssignedTeamID = StatusOpen, PriorityHigh, ChannelEmail, []string{"vip", "billing"}, &teamX
	})
	t2 := mk(day(2), func(tk *Ticket) {
		tk.Status, tk.Priority, tk.Channel, tk.Tags, tk.AssignedTeamID = StatusPendingCustomer, PriorityNormal, ChannelWeb, []string{"billing"}, &teamX
	})
	t3 := mk(day(3), func(tk *Ticket) {
		tk.Status, tk.Priority, tk.Channel, tk.AssignedTeamID = StatusResolved, PriorityHigh, ChannelWhatsApp, &teamY
	})
	t4 := mk(day(4), func(tk *Ticket) {
		tk.Status, tk.Priority, tk.Channel, tk.Tags = StatusOpen, PriorityLow, ChannelWeb, []string{"vip"}
	})
	gone := mk(day(2), func(tk *Ticket) { tk.Tags, tk.AssignedTeamID = []string{"vip"}, &teamX })
	if err := env.repo.DeleteTicket(context.Background(), tenant, gone.ID); err != nil {
		t.Fatal(err)
	}
	// The same values in another tenant never show.
	env.insert(t, other, func(tk *Ticket) { tk.Priority, tk.Tags, tk.AssignedTeamID = PriorityHigh, []string{"vip"}, &teamX })

	open, pending := StatusOpen, StatusPendingCustomer
	high := PriorityHigh
	email := ChannelEmail
	itExpect(t, env, tenant, "no filter", TicketFilter{}, t1, t2, t3, t4)
	itExpect(t, env, tenant, "status", TicketFilter{Status: &open}, t1, t4)
	itExpect(t, env, tenant, "statuses", TicketFilter{Statuses: []TicketStatus{open, pending}}, t1, t2, t4)
	itExpect(t, env, tenant, "priority", TicketFilter{Priority: &high}, t1, t3)
	itExpect(t, env, tenant, "channel", TicketFilter{Channel: &email}, t1)
	itExpect(t, env, tenant, "one tag", TicketFilter{Tags: []string{"vip"}}, t1, t4)
	itExpect(t, env, tenant, "two tags", TicketFilter{Tags: []string{"vip", "billing"}}, t1)
	itExpect(t, env, tenant, "team", TicketFilter{AssignedTeamID: &teamX}, t1, t2)
	itExpect(t, env, tenant, "created range", TicketFilter{CreatedAfter: itPtr(day(2).Add(-time.Hour)), CreatedBefore: itPtr(day(3).Add(time.Hour))}, t2, t3)
	itExpect(t, env, tenant, "priority and team", TicketFilter{Priority: &high, AssignedTeamID: &teamX}, t1)

	tags, err := env.repo.ListTags(context.Background(), tenant, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, tc := range tags {
		counts[tc.Tag] = int(tc.Count)
	}
	if counts["vip"] != 2 || counts["billing"] != 2 || len(counts) != 2 {
		t.Errorf("tags = %v, want vip 2 and billing 2 (deleted and other-tenant tickets left out)", counts)
	}
}

// Search goes through the pg_trgm indexes: subject, description and requester by ILIKE, the
// ticket number exactly, and a short term by prefix without the description.
func TestIntegrationTicketSearch(t *testing.T) {
	env := newITEnv(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	create := func(tenant uuid.UUID, subject, description, email, name string) *Ticket {
		tk, err := env.svc.CreateTicket(ctx, &CreateTicketRequest{
			TenantID: tenant, CustomerID: uuid.New(), Subject: subject, Description: description,
			RequesterEmail: email, RequesterName: name,
		})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	t1 := create(tenant, "Printer jams on the second tray", "Paper keeps folding", "alice@example.com", "Alice Wanjiru")
	t2 := create(tenant, "Invoice total is wrong", "The printer invoice shows VAT twice", "bob@example.com", "Bob")
	t3 := create(tenant, "Login fails", "Cannot sign in after the update", "carol@contoso.test", "Carol")
	create(other, "Printer on fire", "Printer smoke", "dan@example.com", "Dan")
	if t3.TicketNumber != "TKT-000003" {
		t.Fatalf("third ticket of a new tenant is %s", t3.TicketNumber)
	}

	cases := []struct {
		term string
		want []*Ticket
	}{
		{"printer", []*Ticket{t1, t2}}, // subject of t1, description of t2
		{"PRINTER", []*Ticket{t1, t2}},
		{"jams", []*Ticket{t1}},
		{"contoso", []*Ticket{t3}}, // requester email
		{"wanjiru", []*Ticket{t1}}, // requester name
		{"Lo", []*Ticket{t3}},      // short term: subject prefix
		{"in", []*Ticket{t2}},      // short term: not the description ("sign in"), not mid-subject
		{"tkt-2", []*Ticket{t2}},   // ticket number
		{"TKT-000003", []*Ticket{t3}},
		{"nothing matches this", nil},
	}
	for _, c := range cases {
		itExpect(t, env, tenant, "search "+c.term, TicketFilter{Search: c.term}, c.want...)
	}

	// The trigram index can serve the subject match: with sequential scans off the planner still
	// finds an index path, which it could not if the expression and the index disagreed.
	tx, err := env.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, `EXPLAIN SELECT id FROM tickets WHERE `+requesterSearchExpr+` ILIKE $1`, "%contoso%")
	if err != nil {
		t.Fatal(err)
	}
	var plan bytes.Buffer
	for rows.Next() {
		var line string
		_ = rows.Scan(&line)
		plan.WriteString(line + "\n")
	}
	rows.Close()
	if !bytes.Contains(plan.Bytes(), []byte("ticket_requester_trgm")) {
		t.Errorf("requester search does not use ticket_requester_trgm:\n%s", plan.String())
	}
}

// The internal-create key (tenant, source, source_ref.type, source_ref.id) is unique per tenant,
// survives a soft delete, and FindTicketBySourceRef reads it back.
func TestIntegrationSourceRefIdempotency(t *testing.T) {
	env := newITEnv(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	meta := func(source, typ, id string) map[string]any {
		return map[string]any{"source": source, "source_ref": map[string]any{"type": typ, "id": id}}
	}
	first := env.insert(t, tenant, func(tk *Ticket) { tk.Metadata = meta("pos", "order", "A-42") })

	dup := &Ticket{ID: uuid.New(), TicketNumber: "TKT-999999", CustomerID: uuid.New(), Subject: "Again",
		Description: "Again", Status: StatusNew, Priority: PriorityNormal, Channel: ChannelInternal,
		Metadata: meta("pos", "order", "A-42")}
	if err := env.repo.CreateTicket(ctx, tenant, dup); !errors.Is(err, ErrDuplicateSourceRef) {
		t.Fatalf("second create = %v, want ErrDuplicateSourceRef", err)
	}
	found, err := env.repo.FindTicketBySourceRef(ctx, tenant, "pos", SourceRef{Type: "order", ID: "A-42"})
	if err != nil || found.ID != first.ID {
		t.Fatalf("find = %v, %v; want %s", found, err, first.ID)
	}

	// The same reference from another source, of another type, or in another tenant is new.
	env.insert(t, tenant, func(tk *Ticket) { tk.Metadata = meta("ordering", "order", "A-42") })
	env.insert(t, tenant, func(tk *Ticket) { tk.Metadata = meta("pos", "invoice", "A-42") })
	env.insert(t, other, func(tk *Ticket) { tk.Metadata = meta("pos", "order", "A-42") })
	if _, err := env.repo.FindTicketBySourceRef(ctx, tenant, "pos", SourceRef{Type: "order", ID: "missing"}); !errors.Is(err, ErrTicketNotFound) {
		t.Errorf("find unknown ref = %v, want ErrTicketNotFound", err)
	}
	// Tickets without a source_ref never collide.
	env.insert(t, tenant, func(tk *Ticket) { tk.Metadata = map[string]any{"source": "pos"} })
	env.insert(t, tenant, func(tk *Ticket) { tk.Metadata = map[string]any{"source": "pos"} })

	// A soft-deleted ticket still holds its key, so a repeat never opens a second ticket.
	if err := env.repo.DeleteTicket(ctx, tenant, first.ID); err != nil {
		t.Fatal(err)
	}
	dup.ID, dup.TicketNumber = uuid.New(), "TKT-999998"
	if err := env.repo.CreateTicket(ctx, tenant, dup); !errors.Is(err, ErrDuplicateSourceRef) {
		t.Fatalf("create after delete = %v, want ErrDuplicateSourceRef", err)
	}
	if found, err := env.repo.FindTicketBySourceRef(ctx, tenant, "pos", SourceRef{Type: "order", ID: "A-42"}); err != nil || found.ID != first.ID {
		t.Fatalf("find after delete = %v, %v; want the deleted %s", found, err, first.ID)
	}

	// Inside a transaction the duplicate rolls the whole create back, number included.
	before := env.db.Int64(t, `SELECT next_value FROM ticket_sequences WHERE tenant_id = $1`, tenant)
	err = env.repo.InTx(ctx, tenant, func(repo Repository, _ *Outbox) error {
		n, err := repo.NextTicketNumber(ctx, tenant)
		if err != nil {
			return err
		}
		dup.ID, dup.TicketNumber = uuid.New(), n
		return repo.CreateTicket(ctx, tenant, dup)
	})
	if !errors.Is(err, ErrDuplicateSourceRef) {
		t.Fatalf("create in tx = %v, want ErrDuplicateSourceRef", err)
	}
	if after := env.db.Int64(t, `SELECT next_value FROM ticket_sequences WHERE tenant_id = $1`, tenant); after != before {
		t.Errorf("counter moved from %d to %d by a rolled-back create", before, after)
	}
}

// A channel message is stored once per (tenant, channel, external_id).
func TestIntegrationCommentDedupe(t *testing.T) {
	env := newITEnv(t)
	ctx := context.Background()
	tenant, other := uuid.New(), uuid.New()
	tk := env.insert(t, tenant, nil)
	otherTk := env.insert(t, other, nil)
	comment := func(tenant, ticketID uuid.UUID, channel, externalID string) *TicketComment {
		meta := map[string]any{}
		if channel != "" {
			meta["channel"] = channel
		}
		if externalID != "" {
			meta["external_id"] = externalID
		}
		return &TicketComment{ID: uuid.New(), TicketID: ticketID, AuthorType: AuthorCustomer,
			CommentType: CommentPublic, Content: "Hello", Metadata: meta}
	}

	first := comment(tenant, tk.ID, "email", "<m1@mail.test>")
	if err := env.repo.CreateComment(ctx, tenant, first); err != nil {
		t.Fatal(err)
	}
	if err := env.repo.CreateComment(ctx, tenant, comment(tenant, tk.ID, "email", "<m1@mail.test>")); !errors.Is(err, ErrDuplicateMessage) {
		t.Fatalf("redelivered message = %v, want ErrDuplicateMessage", err)
	}
	for _, c := range []*TicketComment{
		comment(tenant, tk.ID, "whatsapp", "<m1@mail.test>"),           // same id, other channel
		comment(other, otherTk.ID, "email", "<m1@mail.test>"),          // same message, other tenant
		comment(tenant, tk.ID, "", ""), comment(tenant, tk.ID, "", ""), // agent replies carry no key
	} {
		tenantOf := tenant
		if c.TicketID == otherTk.ID {
			tenantOf = other
		}
		if err := env.repo.CreateComment(ctx, tenantOf, c); err != nil {
			t.Errorf("create %v: %v", c.Metadata, err)
		}
	}

	got, err := env.repo.FindCommentByExternalID(ctx, tenant, "email", "<m1@mail.test>")
	if err != nil || got.ID != first.ID {
		t.Fatalf("find = %v, %v; want %s", got, err, first.ID)
	}
	// A deleted message still counts, so a late redelivery is still refused.
	if err := env.repo.DeleteComment(ctx, tenant, first.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := env.repo.FindCommentByExternalID(ctx, tenant, "email", "<m1@mail.test>"); err != nil || got.ID != first.ID {
		t.Fatalf("find after delete = %v, %v", got, err)
	}
	if err := env.repo.CreateComment(ctx, tenant, comment(tenant, tk.ID, "email", "<m1@mail.test>")); !errors.Is(err, ErrDuplicateMessage) {
		t.Fatalf("redelivery after delete = %v, want ErrDuplicateMessage", err)
	}
}

// No read or write of one tenant reaches another tenant's rows.
func TestIntegrationTenantIsolation(t *testing.T) {
	env := newITEnv(t)
	ctx := context.Background()
	owner, intruder := uuid.New(), uuid.New()
	tk := env.insert(t, owner, func(tk *Ticket) {
		tk.Tags = []string{"secret"}
		tk.Metadata = map[string]any{"source": "pos", "source_ref": map[string]any{"type": "order", "id": "X-1"}}
		tk.SLAResponseDueAt = itPtr(time.Now().Add(-time.Hour))
	})
	c := &TicketComment{ID: uuid.New(), TicketID: tk.ID, AuthorType: AuthorCustomer, CommentType: CommentPublic,
		Content: "Private", Metadata: map[string]any{"channel": "email", "external_id": "<x@y>"}}
	if err := env.repo.CreateComment(ctx, owner, c); err != nil {
		t.Fatal(err)
	}
	// The intruder has a ticket of their own with the same number.
	env.insert(t, intruder, nil)

	notFound := func(label string, err error, want error) {
		t.Helper()
		if !errors.Is(err, want) {
			t.Errorf("%s across tenants = %v, want %v", label, err, want)
		}
	}
	_, err := env.repo.GetTicket(ctx, intruder, tk.ID)
	notFound("GetTicket", err, ErrTicketNotFound)
	if got, err := env.repo.GetTicketByNumber(ctx, intruder, tk.TicketNumber); err != nil || got.TenantID != intruder {
		t.Errorf("GetTicketByNumber returned %v, %v; want the intruder's own ticket", got, err)
	}
	_, err = env.repo.FindTicketBySourceRef(ctx, intruder, "pos", SourceRef{Type: "order", ID: "X-1"})
	notFound("FindTicketBySourceRef", err, ErrTicketNotFound)
	_, err = env.repo.GetComment(ctx, intruder, c.ID)
	notFound("GetComment", err, ErrCommentNotFound)
	_, err = env.repo.FindCommentByExternalID(ctx, intruder, "email", "<x@y>")
	notFound("FindCommentByExternalID", err, ErrCommentNotFound)
	_, err = env.repo.ListComments(ctx, intruder, tk.ID, CommentQuery{Page: PageParams{Limit: 10}})
	notFound("ListComments", err, ErrTicketNotFound)
	notFound("CreateComment", env.repo.CreateComment(ctx, intruder, &TicketComment{ID: uuid.New(), TicketID: tk.ID,
		AuthorType: AuthorAgent, CommentType: CommentPublic, Content: "x"}), ErrTicketNotFound)
	notFound("UpdateTicket", env.repo.UpdateTicket(ctx, intruder, tk.ID, &UpdateTicketRequest{Subject: itPtr("pwned")}), ErrTicketNotFound)
	notFound("UpdateTicketStatus", env.repo.UpdateTicketStatus(ctx, intruder, tk.ID, StatusClosed), ErrTicketNotFound)
	notFound("AssignTicket", env.repo.AssignTicket(ctx, intruder, tk.ID, nil, nil), ErrTicketNotFound)
	notFound("UpdateSLA", env.repo.UpdateSLA(ctx, intruder, tk.ID, SLAUpdate{Resume: true}), ErrTicketNotFound)
	notFound("UpdateComment", env.repo.UpdateComment(ctx, intruder, c.ID, "pwned"), ErrCommentNotFound)
	notFound("DeleteComment", env.repo.DeleteComment(ctx, intruder, c.ID), ErrCommentNotFound)
	notFound("DeleteTicket", env.repo.DeleteTicket(ctx, intruder, tk.ID), ErrTicketNotFound)
	if changed, err := env.repo.MarkSLABreached(ctx, intruder, tk.ID, true, false); err != nil || changed {
		t.Errorf("MarkSLABreached across tenants changed=%v err=%v", changed, err)
	}
	if changed, err := env.repo.MarkSLAWarned(ctx, intruder, tk.ID, "first_response", time.Now()); err != nil || changed {
		t.Errorf("MarkSLAWarned across tenants changed=%v err=%v", changed, err)
	}
	if err := env.repo.MoveThread(ctx, intruder, tk.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}

	page, err := env.svc.ListTickets(ctx, intruder, TicketFilter{Search: tk.Subject, Page: PageParams{Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page.Data {
		if row.TenantID != intruder {
			t.Errorf("ListTickets of the intruder returned a ticket of tenant %s", row.TenantID)
		}
	}
	tags, err := env.repo.ListTags(ctx, intruder, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Errorf("ListTags of the intruder = %v", tags)
	}

	// The owner's rows are as they were.
	after := env.get(t, owner, tk.ID)
	if after.Subject != tk.Subject || after.Status != tk.Status || after.SLAResponseBreached {
		t.Errorf("owner's ticket changed: %+v", after)
	}
	got, err := env.repo.GetComment(ctx, owner, c.ID)
	if err != nil || got.Content != "Private" || got.TicketID != tk.ID {
		t.Errorf("owner's comment changed: %+v, %v", got, err)
	}
}
