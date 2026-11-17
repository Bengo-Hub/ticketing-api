package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
)

var testAddrs = Addresses{SupportDomain: "support.codevertexafrica.com", PlatformAddress: "support@codevertexafrica.com", PlatformSlug: "codevertex"}

// fakeLookup answers like the shared tenant cache: a known slug, auth-api's 404, or an outage.
type fakeLookup struct {
	ids  map[string]string
	down bool
}

func (f fakeLookup) GetTenantDetails(_ context.Context, slug string) (sharedcache.TenantDetails, error) {
	if f.down {
		return sharedcache.TenantDetails{}, errors.New("cache.fetchTenant: auth-api HTTP 503")
	}
	if id, ok := f.ids[slug]; ok {
		return sharedcache.TenantDetails{ID: id, Slug: slug}, nil
	}
	return sharedcache.TenantDetails{}, fmt.Errorf("cache.fetchTenant: tenant %q not found (404)", slug)
}

func TestTenantFromRecipient(t *testing.T) {
	ctx := context.Background()
	platform, hadia := uuid.New(), uuid.New()
	r := NewTenantResolver(testAddrs, fakeLookup{ids: map[string]string{"codevertex": platform.String(), "hadia": hadia.String()}})

	cases := []struct {
		name       string
		candidates []string
		want       uuid.UUID
		permanent  bool
	}{
		{"platform support address", []string{"Support@CodevertexAfrica.com"}, platform, false},
		{"tenant forwarding address", []string{"hadia@support.codevertexafrica.com"}, hadia, false},
		{"plus tag ignored", []string{"hadia+billing@support.codevertexafrica.com"}, hadia, false},
		{"recipient empty, found in To", []string{"", "someone@else.com", "Hadia <hadia@support.codevertexafrica.com>"}, hadia, false},
		{"unknown slug", []string{"nobody@support.codevertexafrica.com"}, uuid.Nil, true},
		{"not a support address", []string{"info@codevertexafrica.com"}, uuid.Nil, true},
		{"slug with a bad character", []string{"ha_dia@support.codevertexafrica.com"}, uuid.Nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, _, err := r.Resolve(ctx, tc.candidates...)
			if tc.permanent {
				if !IsPermanent(err) {
					t.Fatalf("want a permanent error, got %v", err)
				}
				return
			}
			if err != nil || id != tc.want {
				t.Fatalf("got %v %v, want %v", id, err, tc.want)
			}
		})
	}

	down := NewTenantResolver(testAddrs, fakeLookup{down: true})
	if _, _, err := down.Resolve(ctx, "hadia@support.codevertexafrica.com"); err == nil || IsPermanent(err) {
		t.Fatalf("an auth-api outage must be retried, got %v", err)
	}
	if got := testAddrs.ForwardingAddress("hadia"); got != "hadia@support.codevertexafrica.com" {
		t.Fatalf("forwarding address = %q", got)
	}
	if got := testAddrs.ForwardingAddress("codevertex"); got != "support@codevertexafrica.com" {
		t.Fatalf("platform forwarding address = %q", got)
	}
}

func baseEmail() EmailInbound {
	return EmailInbound{
		MessageID: "<abc@mail.example.com>", FromEmail: "Amina@Example.com", FromName: "Amina",
		Recipient: "hadia@support.codevertexafrica.com", Subject: "Re: Payout late [TKT-42]", Text: "Any news?",
	}
}

// Auto-replies, bounces, no-reply senders, our own mail and spam are refused.
func TestAutoReplyRefusal(t *testing.T) {
	cases := map[string]func(e *EmailInbound){
		"auto-submitted field":  func(e *EmailInbound) { e.AutoSubmitted = "auto-replied" },
		"auto-submitted header": func(e *EmailInbound) { e.Headers = map[string]string{"auto-submitted": "auto-generated"} },
		"x-autoreply header":    func(e *EmailInbound) { e.Headers = map[string]string{"x-autoreply": "yes"} },
		"precedence bulk":       func(e *EmailInbound) { e.Headers = map[string]string{"precedence": "bulk"} },
		"no-reply sender":       func(e *EmailInbound) { e.FromEmail = "no-reply@shop.example.com" },
		"mailer daemon":         func(e *EmailInbound) { e.FromEmail = "MAILER-DAEMON@mx.example.com" },
		"delivery status report": func(e *EmailInbound) {
			e.Headers = map[string]string{"content-type": "multipart/report; report-type=delivery-status"}
		},
		"out of office subject":   func(e *EmailInbound) { e.Subject = "Out of Office: Payout late" },
		"automatic reply subject": func(e *EmailInbound) { e.Subject = "Automatic reply: Payout late" },
		"our own forwarding mail": func(e *EmailInbound) { e.FromEmail = "hadia@support.codevertexafrica.com" },
		"spam flag":               func(e *EmailInbound) { e.Spam = true },
		"virus flag":              func(e *EmailInbound) { e.Virus = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := baseEmail()
			mutate(&e)
			m, ok := EmailMessage(e, tenant, "hadia", testAddrs)
			if !ok || m.Refusal == "" {
				t.Fatalf("want a refusal, got %+v", m)
			}
		})
	}
	m, ok := EmailMessage(baseEmail(), tenant, "hadia", testAddrs)
	if !ok || m.Refusal != "" {
		t.Fatalf("an ordinary reply must not be refused: %q", m.Refusal)
	}
	person := baseEmail()
	person.AutoSubmitted = "no"
	if m, _ := EmailMessage(person, tenant, "hadia", testAddrs); m.Refusal != "" {
		t.Fatal("Auto-Submitted: no is a person writing")
	}
}

// The event maps to a Message: the id without brackets, the thread order, the tag, the sender.
func TestEmailMessageFields(t *testing.T) {
	e := baseEmail()
	e.InReplyTo = "<parent@x.example.com>"
	e.References = []string{"<root@x.example.com> <middle@x.example.com>", "<parent@x.example.com>"}
	m, _ := EmailMessage(e, tenant, "hadia", testAddrs)
	if m.ExternalID != "abc@mail.example.com" || m.Requester.Email != "amina@example.com" || m.TicketNumber != "TKT-000042" {
		t.Fatalf("message = %+v", m)
	}
	want := []string{"parent@x.example.com", "middle@x.example.com", "root@x.example.com"}
	if strings.Join(m.ThreadRefs, ",") != strings.Join(want, ",") {
		t.Fatalf("thread refs = %v, want %v", m.ThreadRefs, want)
	}
	e.MessageID = ""
	a, _ := EmailMessage(e, tenant, "hadia", testAddrs)
	b, _ := EmailMessage(e, tenant, "hadia", testAddrs)
	if a.ExternalID == "" || a.ExternalID != b.ExternalID {
		t.Fatal("mail without a Message-ID still gets a stable dedupe id")
	}
}

// Only keys in the tenant's own inbound mail prefix (email-provisioner's layout) are recorded.
func TestAttachmentKeysStayInTheTenant(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef"
	in := []EmailAttachment{
		{Key: "support-inbound/hadia/inbound-mail/" + hash + "/01-a.pdf", Filename: "a.pdf"},
		{Key: "support-inbound/hadia/inbound-mail/" + hash + "/02-b.pdf", Filename: "b.pdf"},
		{Key: "support-inbound/other/inbound-mail/" + hash + "/03-c.pdf"},
		{Key: "support-inbound/hadia/kyc/" + hash + "/04-d.pdf"},
		{Key: "email-provisioner/hadia/inbound-mail/" + hash + "/05-e.pdf"},
		{Key: "support-inbound/hadia/inbound-mail/../../x"},
		{Key: "support-inbound/hadia/inbound-mail/" + hash + "/06-f.pdf", ScanStatus: "infected"},
	}
	got := emailFiles(in, attachmentOwner("hadia", testAddrs))
	if len(got) != 2 || got[0].Filename != "a.pdf" || got[1].Filename != "b.pdf" {
		t.Fatalf("files = %+v", got)
	}
	// The platform tenant's own mailbox writes under the "platform" owner.
	plat := emailFiles([]EmailAttachment{{Key: "support-inbound/platform/inbound-mail/" + hash + "/01-p.pdf", Filename: "p.pdf"}},
		attachmentOwner("codevertex", testAddrs))
	if len(plat) != 1 {
		t.Fatalf("platform files = %+v", plat)
	}
}

func TestBodyHelpers(t *testing.T) {
	reply := "Thanks, that worked.\n\nOn Mon, 17 Nov 2026 at 10:00, Support <support@x.com>\nwrote:\n> old text"
	if got := stripQuoted(reply); got != "Thanks, that worked." {
		t.Fatalf("stripQuoted = %q", got)
	}
	if got := textToHTML("a < b\nnext\n\nsecond"); got != "<p>a &lt; b<br>next</p><p>second</p>" {
		t.Fatalf("textToHTML = %q", got)
	}
	id := uuid.New()
	if got, ok := OwnToken("<" + id.String() + "@mail.tenant.com>"); !ok || got != id {
		t.Fatal("our own Message-ID token must parse")
	}
	if _, ok := OwnToken("<1731.abc@mail.tenant.com>"); ok {
		t.Fatal("a provider-generated id is not ours")
	}
	if got := ticketSubject(Message{Subject: "RE: Fwd: Payout late [TKT-7]"}); got != "Payout late" {
		t.Fatalf("subject = %q", got)
	}
}
