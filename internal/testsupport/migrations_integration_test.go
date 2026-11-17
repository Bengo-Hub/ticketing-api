//go:build integration

package testsupport

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { Main(m, "it_migrate") }

// The migrations apply to an empty schema (Main already did it), and the schema they build is the
// one the Ent schema describes: Ent finds nothing left to create or change.
func TestMigrationsMatchEntSchema(t *testing.T) {
	db := Open(t)
	var version string
	if err := db.SQL.QueryRow("SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("PostgreSQL %s, schema %s", version, db.Schema)

	var buf bytes.Buffer
	if err := db.Ent.Schema.WriteTo(context.Background(), &buf); err != nil {
		t.Fatalf("ent diff: %v", err)
	}
	var drift []string
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "", line == "BEGIN;", line == "COMMIT;", strings.HasPrefix(line, "--"):
			continue
		}
		drift = append(drift, line)
	}
	if len(drift) > 0 {
		t.Errorf("the Ent schema has changes no migration makes; generate a migration for:\n%s", strings.Join(drift, "\n"))
	}
}

// The expression and partial indexes Ent cannot declare exist only in the migration files. The
// code that relies on each one names it; this checks every one of them is really created.
func TestMigrationsCreateHandWrittenIndexes(t *testing.T) {
	db := Open(t)
	want := map[string]string{
		"ticket_tenant_source_ref":                 "internal create idempotency (tickets.FindTicketBySourceRef)",
		"ticketcomment_tenant_channel_external_id": "channel message dedupe (tickets.CreateComment)",
		"ticket_requester_trgm":                    "requester search (tickets.requesterSearchExpr)",
		"ticket_subject_trgm":                      "subject search",
		"ticket_description_trgm":                  "description search",
		"ticket_tags_gin":                          "tag filter",
		"ticket_tenant_requester_phone":            "WhatsApp threading (tickets.LatestOpenTicketByPhone)",
		"ticket_open_backlog":                      "analytics live backlog (analytics.LiveStats)",
		"ticket_updated_at_id":                     "rollup change scan (analytics.Rollup)",
		"ticket_tenant_sla_due":                    "sort=sla_due (tickets/sla_sort.go)",
	}
	rows, err := db.SQL.Query(`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema()`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, usedBy := range want {
		if !have[name] {
			t.Errorf("index %s (used by %s) is not created by any migration", name, usedBy)
		}
	}
	// The global ticket number key was replaced by the per-tenant one.
	if have["tickets_ticket_number_key"] {
		t.Error("tickets_ticket_number_key still exists; ticket numbers would collide across tenants")
	}
	if !have["ticket_tenant_id_ticket_number"] {
		t.Error("ticket_tenant_id_ticket_number is missing")
	}
}
