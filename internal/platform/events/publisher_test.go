package events

import "testing"

// The background publisher drains the same outbox repository the writer inserts into.
func TestOutboxRepoIsExposed(t *testing.T) {
	if NewOutbox(nil).OutboxRepo() == nil {
		t.Fatal("the background publisher needs the outbox repository")
	}
}
