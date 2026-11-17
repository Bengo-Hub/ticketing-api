package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/Bengo-Hub/shared-service-client/crm"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// fakeContacts answers the CRM upsert and records what it was asked.
type fakeContacts struct {
	id    uuid.UUID
	err   error
	asked []crm.UpsertInput
}

func (f *fakeContacts) UpsertStrict(_ context.Context, in crm.UpsertInput) (crm.Contact, error) {
	f.asked = append(f.asked, in)
	if f.err != nil {
		return crm.Contact{}, f.err
	}
	return crm.Contact{ID: f.id}, nil
}

// A new ticket is linked to the requester's marketflow contact, looked up by email or phone with
// the name split the way marketflow keeps it.
func TestCreateLinksCRMContact(t *testing.T) {
	f := newFakeTickets()
	s := newTestService(f)
	contacts := &fakeContacts{id: uuid.New()}
	s.SetContacts(contacts)
	m := emailMsg("new-1@mail.example.com")
	m.Requester.Name = "Amina Wanjiru Otieno"
	if res, err := s.Ingest(context.Background(), m); err != nil || res.Outcome != OutcomeCreated {
		t.Fatalf("got %+v %v", res, err)
	}
	if len(contacts.asked) != 1 {
		t.Fatalf("asked %d times", len(contacts.asked))
	}
	in := contacts.asked[0]
	if in.TenantID != tenant || in.Email != "amina@example.com" || in.FirstName != "Amina" || in.LastName != "Wanjiru Otieno" {
		t.Fatalf("upsert input = %+v", in)
	}
	if got := f.created[0].CRMContactID; got == nil || *got != contacts.id {
		t.Fatalf("crm contact id = %v", got)
	}
}

// A CRM that fails or is not configured never holds the ticket back: it opens without a contact id.
func TestCreateWithoutCRMContact(t *testing.T) {
	for _, err := range []error{&crm.Error{Status: 500, Message: "boom"}, crm.ErrDisabled} {
		f := newFakeTickets()
		s := newTestService(f)
		s.SetContacts(&fakeContacts{err: err})
		res, ingestErr := s.Ingest(context.Background(), emailMsg("new-2@mail.example.com"))
		if ingestErr != nil || res.Outcome != OutcomeCreated {
			t.Fatalf("%v: got %+v %v", err, res, ingestErr)
		}
		if f.created[0].CRMContactID != nil {
			t.Fatalf("%v: crm contact id set", err)
		}
	}
	// No client wired at all.
	f := newFakeTickets()
	if _, err := newTestService(f).Ingest(context.Background(), emailMsg("new-3@mail.example.com")); err != nil || f.created[0].CRMContactID != nil {
		t.Fatalf("err %v, id %v", err, f.created[0].CRMContactID)
	}
}

// A message that continues a ticket asks the CRM nothing.
func TestThreadSkipsCRM(t *testing.T) {
	f := newFakeTickets()
	open := f.ticket("TKT-000005", tickets.StatusOpen, "amina@example.com", "")
	s := newTestService(f)
	contacts := &fakeContacts{id: uuid.New()}
	s.SetContacts(contacts)
	m := emailMsg("reply-5@mail.example.com")
	m.ThreadRefs = []string{open.ID.String() + "@tenant.example.com"}
	if res, err := s.Ingest(context.Background(), m); err != nil || res.Outcome != OutcomeThreaded {
		t.Fatalf("got %+v %v", res, err)
	}
	if len(contacts.asked) != 0 {
		t.Fatal("a threaded message must not call the CRM")
	}
}

// The WhatsApp number and receive time reach the tickets service on both paths, for the reply window.
func TestWhatsAppWindowFieldsPassThrough(t *testing.T) {
	at := time.Date(2026, 11, 17, 11, 0, 0, 0, time.UTC)
	m, ok := WhatsAppMessage(WhatsAppInbound{TenantID: tenant.String(), PhoneNumberID: "pn-1", WAMessageID: "wamid.1",
		From: "+254712345678", Type: "text", Text: "hello", ReceivedAt: at.Format(time.RFC3339)}, true)
	if !ok || m.PhoneNumberID != "pn-1" {
		t.Fatalf("message = %+v", m)
	}

	f := newFakeTickets()
	if _, err := newTestService(f).Ingest(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if o := f.created[0].Inbound; o.PhoneNumberID != "pn-1" || !o.ReceivedAt.Equal(at) {
		t.Fatalf("origin = %+v", o)
	}

	f = newFakeTickets()
	f.latest = f.ticket("TKT-000006", tickets.StatusOpen, "", "+254712345678")
	m.ExternalID = "wamid.2"
	if _, err := newTestService(f).Ingest(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if in := f.added[0]; in.PhoneNumberID != "pn-1" || !in.ReceivedAt.Equal(at) {
		t.Fatalf("inbound = %+v", in)
	}
}
