package settings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// memStore keeps sections in memory with the same key-level writes and timed touch as Store.
type memStore struct {
	doc     map[string]map[string]any
	touches int
	fail    error
}

func newMemStore() *memStore { return &memStore{doc: map[string]map[string]any{}} }

func (m *memStore) Section(_ context.Context, _ uuid.UUID, name string, into any) (bool, error) {
	sec, ok := m.doc[name]
	if !ok {
		return false, nil
	}
	raw, _ := json.Marshal(sec)
	return true, json.Unmarshal(raw, into)
}

func (m *memStore) PutSectionKey(_ context.Context, _ uuid.UUID, section, key string, v any) error {
	raw, _ := json.Marshal(v)
	var val any
	_ = json.Unmarshal(raw, &val)
	if m.doc[section] == nil {
		m.doc[section] = map[string]any{}
	}
	m.doc[section][key] = val
	return nil
}

func (m *memStore) TouchSectionTime(_ context.Context, _ uuid.UUID, section, object, key string, at time.Time, every time.Duration) error {
	if m.fail != nil {
		return m.fail
	}
	m.touches++
	if m.doc[section] == nil {
		m.doc[section] = map[string]any{}
	}
	obj, _ := m.doc[section][object].(map[string]any)
	if obj == nil {
		obj = map[string]any{}
	}
	if s, ok := obj[key].(string); ok {
		if last, err := time.Parse(time.RFC3339, s); err == nil && !last.Before(at.Add(-every)) {
			return nil
		}
	}
	obj[key] = at.UTC().Format(time.RFC3339)
	m.doc[section][object] = obj
	return nil
}

var tenantA = uuid.MustParse("6f1d7a52-2a8e-4d55-9a43-0b0c1e2f3a4b")

// A tenant's receipt is written at most once a minute however much mail arrives, and a failed
// write is tried again on the next message.
func TestEmailReceivedThrottled(t *testing.T) {
	store := newMemStore()
	s := NewService(store)
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	ctx := context.Background()

	for range 5 {
		if err := s.EmailReceived(ctx, tenantA); err != nil {
			t.Fatal(err)
		}
	}
	if store.touches != 1 {
		t.Fatalf("touches = %d, want 1", store.touches)
	}
	c, _ := s.Channels(ctx, tenantA)
	if c.Email.LastReceivedAt == nil || !c.Email.LastReceivedAt.Equal(now) {
		t.Fatalf("last_received_at = %v", c.Email.LastReceivedAt)
	}

	now = now.Add(EmailReceiptEvery)
	if err := s.EmailReceived(ctx, tenantA); err != nil || store.touches != 2 {
		t.Fatalf("after a minute: err %v, touches %d", err, store.touches)
	}

	store.fail = errors.New("db down")
	now = now.Add(2 * EmailReceiptEvery)
	if err := s.EmailReceived(ctx, tenantA); err == nil {
		t.Fatal("want the store error")
	}
	store.fail = nil
	if err := s.EmailReceived(ctx, tenantA); err != nil || store.touches != 3 {
		t.Fatalf("retry after a failure: err %v, touches %d", err, store.touches)
	}
}

// Saving the WhatsApp settings keeps the email receipt.
func TestSetWhatsAppKeepsReceipt(t *testing.T) {
	store := newMemStore()
	s := NewService(store)
	ctx := context.Background()
	if err := s.EmailReceived(ctx, tenantA); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWhatsApp(ctx, tenantA, WhatsAppChannel{AutoTicket: true}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Channels(ctx, tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if !c.WhatsApp.AutoTicket || c.Email.LastReceivedAt == nil {
		t.Fatalf("channels = %+v", c)
	}
}
