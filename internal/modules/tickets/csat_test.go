package tickets

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// csatRepo keeps one ticket and applies the survey writes the way the SQL does.
type csatRepo struct {
	Repository
	ticket    *Ticket
	responses []*CSATResponse
}

func (r *csatRepo) InTx(_ context.Context, _ uuid.UUID, fn func(Repository, *Outbox) error) error {
	return fn(r, &Outbox{})
}

func (r *csatRepo) GetTicket(_ context.Context, _ uuid.UUID, id uuid.UUID) (*Ticket, error) {
	if r.ticket == nil || r.ticket.ID != id {
		return nil, ErrTicketNotFound
	}
	cp := *r.ticket
	cp.Metadata = maps.Clone(r.ticket.Metadata)
	return &cp, nil
}

func (r *csatRepo) MergeTicketMetadata(_ context.Context, _ uuid.UUID, _ uuid.UUID, patch map[string]any) error {
	if r.ticket.Metadata == nil {
		r.ticket.Metadata = map[string]any{}
	}
	maps.Copy(r.ticket.Metadata, patch)
	return nil
}

func (r *csatRepo) ConsumeCSATToken(_ context.Context, _ uuid.UUID, _ uuid.UUID, hash string, now time.Time) (bool, error) {
	st, ok := readCSATState(r.ticket.Metadata)
	if !ok || st.TokenHash != hash || st.Used || !now.Before(st.ExpiresAt) {
		return false, nil
	}
	m := r.ticket.Metadata[MetaCSAT].(map[string]any)
	m["used_at"] = now.UTC().Format(time.RFC3339)
	return true, nil
}

func (r *csatRepo) CreateCSATResponse(_ context.Context, resp *CSATResponse) error {
	r.responses = append(r.responses, resp)
	return nil
}

func newCSATFixture(t *testing.T, email string) (*Service, *csatRepo, *Outbox) {
	t.Helper()
	tk := &Ticket{
		ID: uuid.New(), TenantID: uuid.New(), TicketNumber: "TKT-000042", Subject: "Printer jam",
		Status: StatusOpen, Priority: PriorityNormal, Channel: ChannelEmail,
		Metadata: map[string]any{MetaRequesterEmail: email},
	}
	repo := &csatRepo{ticket: tk}
	svc := NewService(repo, zap.NewNop())
	svc.SetCSAT(CSATConfig{SurveyURL: "https://ticketing.example/support/csat"})
	return svc, repo, &Outbox{}
}

// issue runs csatTx at resolvedAt and returns the token from the event.
func issue(t *testing.T, svc *Service, repo *csatRepo, out *Outbox, resolvedAt time.Time) string {
	t.Helper()
	if err := svc.csatTx(context.Background(), repo, out, repo.ticket, resolvedAt); err != nil {
		t.Fatalf("csatTx: %v", err)
	}
	if len(out.Events) == 0 {
		t.Fatal("no csat event recorded")
	}
	ev := out.Events[len(out.Events)-1]
	if ev.Type != EventTicketCSATRequested || ev.Aggregate != AggregateTicketing {
		t.Fatalf("event = %s.%s", ev.Aggregate, ev.Type)
	}
	return ev.Data["csat_token"].(string)
}

func TestCSATIssueStoresOnlyTheHash(t *testing.T) {
	svc, repo, out := newCSATFixture(t, "jane@example.com")
	resolved := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	token := issue(t, svc, repo, out, resolved)

	st, ok := readCSATState(repo.ticket.Metadata)
	if !ok || st.TokenHash != csatTokenHash(token) || st.TokenHash == token {
		t.Fatalf("metadata csat = %+v", repo.ticket.Metadata[MetaCSAT])
	}
	if want := resolved.Add(DefaultCSATTokenTTL); !st.ExpiresAt.Equal(want) {
		t.Fatalf("expires_at = %v, want %v", st.ExpiresAt, want)
	}
	data := out.Events[0].Data
	if data["survey_channel"] != "email" || data["requester_email"] != "jane@example.com" || data["status"] != "resolved" {
		t.Fatalf("payload = %v", data)
	}
	u, err := url.Parse(data["csat_url"].(string))
	if err != nil || u.Query().Get("token") != token || u.Query().Get("tenant") != repo.ticket.TenantID.String() {
		t.Fatalf("csat_url = %v", data["csat_url"])
	}
}

func TestCSATNoSurveyWithoutEmailOrURL(t *testing.T) {
	svc, repo, out := newCSATFixture(t, "")
	if err := svc.csatTx(context.Background(), repo, out, repo.ticket, time.Now()); err != nil || len(out.Events) != 0 {
		t.Fatalf("no email: err=%v events=%d", err, len(out.Events))
	}
	svc, repo, out = newCSATFixture(t, "jane@example.com")
	svc.SetCSAT(CSATConfig{})
	if err := svc.csatTx(context.Background(), repo, out, repo.ticket, time.Now()); err != nil || len(out.Events) != 0 {
		t.Fatalf("no url: err=%v events=%d", err, len(out.Events))
	}
}

func TestCSATTokenWorksOnce(t *testing.T) {
	svc, repo, out := newCSATFixture(t, "jane@example.com")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now.Add(time.Hour) }
	token := issue(t, svc, repo, out, now)
	ctx := context.Background()

	survey, err := svc.CheckCSAT(ctx, repo.ticket.TenantID, token)
	if err != nil || survey.TicketNumber != "TKT-000042" {
		t.Fatalf("check: %v %+v", err, survey)
	}
	if err := svc.SubmitCSAT(ctx, repo.ticket.TenantID, token, 5, "  quick fix  "); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if len(repo.responses) != 1 || repo.responses[0].Score != 5 || repo.responses[0].Comment != "quick fix" {
		t.Fatalf("responses = %+v", repo.responses)
	}
	if err := svc.SubmitCSAT(ctx, repo.ticket.TenantID, token, 1, ""); !errors.Is(err, ErrCSATTokenUsed) {
		t.Fatalf("second submit = %v, want ErrCSATTokenUsed", err)
	}
	if _, err := svc.CheckCSAT(ctx, repo.ticket.TenantID, token); !errors.Is(err, ErrCSATTokenUsed) {
		t.Fatalf("check after use = %v", err)
	}
	if len(repo.responses) != 1 {
		t.Fatalf("a used token stored another response")
	}
}

func TestCSATTokenExpires(t *testing.T) {
	svc, repo, out := newCSATFixture(t, "jane@example.com")
	resolved := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	token := issue(t, svc, repo, out, resolved)
	svc.now = func() time.Time { return resolved.Add(DefaultCSATTokenTTL) }
	if err := svc.SubmitCSAT(context.Background(), repo.ticket.TenantID, token, 4, ""); !errors.Is(err, ErrCSATTokenExpired) {
		t.Fatalf("submit at expiry = %v, want ErrCSATTokenExpired", err)
	}
}

func TestCSATReissueReplacesOldToken(t *testing.T) {
	svc, repo, out := newCSATFixture(t, "jane@example.com")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now.Add(time.Hour) }
	old := issue(t, svc, repo, out, now)
	fresh := issue(t, svc, repo, out, now.Add(30*time.Minute))
	if err := svc.SubmitCSAT(context.Background(), repo.ticket.TenantID, old, 3, ""); !errors.Is(err, ErrCSATTokenInvalid) {
		t.Fatalf("old token = %v, want ErrCSATTokenInvalid", err)
	}
	if err := svc.SubmitCSAT(context.Background(), repo.ticket.TenantID, fresh, 3, ""); err != nil {
		t.Fatalf("new token: %v", err)
	}
}

func TestCSATRejectsBadInput(t *testing.T) {
	svc, repo, out := newCSATFixture(t, "jane@example.com")
	now := time.Now()
	svc.now = func() time.Time { return now }
	token := issue(t, svc, repo, out, now)
	ctx := context.Background()
	for _, score := range []int{0, 6, -1} {
		if err := svc.SubmitCSAT(ctx, repo.ticket.TenantID, token, score, ""); !errors.Is(err, ErrCSATScore) {
			t.Fatalf("score %d = %v", score, err)
		}
	}
	long := make([]rune, MaxCSATCommentRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	if err := svc.SubmitCSAT(ctx, repo.ticket.TenantID, token, 4, string(long)); !errors.Is(err, ErrCSATComment) {
		t.Fatalf("long comment = %v", err)
	}
	for _, bad := range []string{"", "not-a-token", token[:len(token)-2]} {
		if err := svc.SubmitCSAT(ctx, repo.ticket.TenantID, bad, 4, ""); !errors.Is(err, ErrCSATTokenInvalid) {
			t.Fatalf("token %q = %v", bad, err)
		}
	}
	// A well-formed token for another ticket is unknown here.
	other, _, _ := newCSATToken(uuid.New())
	if err := svc.SubmitCSAT(ctx, repo.ticket.TenantID, other, 4, ""); !errors.Is(err, ErrCSATTokenInvalid) {
		t.Fatalf("foreign token = %v", err)
	}
	if len(repo.responses) != 0 {
		t.Fatalf("bad input stored a response")
	}
}
