-- Create index "agent_tenant_id_created_at_id" to table: "agents"
CREATE INDEX "agent_tenant_id_created_at_id" ON "agents" ("tenant_id", "created_at", "id");
-- Create index "kbarticle_tenant_id_created_at_id" to table: "kb_articles"
CREATE INDEX "kbarticle_tenant_id_created_at_id" ON "kb_articles" ("tenant_id", "created_at", "id");
-- Create index "team_tenant_id_created_at_id" to table: "teams"
CREATE INDEX "team_tenant_id_created_at_id" ON "teams" ("tenant_id", "created_at", "id");
-- Create index "ticket_tenant_sla_due" to table: "tickets" (hand-written expression index for
-- sort=sla_due; ent cannot declare it, see internal/modules/tickets/sla_sort.go)
CREATE INDEX "ticket_tenant_sla_due" ON "tickets" ("tenant_id", ((CASE WHEN "sla_paused_at" IS NULL THEN 0 ELSE 2 END) + (CASE WHEN LEAST((CASE WHEN "first_response_at" IS NULL THEN "sla_response_due_at" END), "sla_resolution_due_at") IS NULL THEN 1 ELSE 0 END)), (COALESCE(LEAST((CASE WHEN "first_response_at" IS NULL THEN "sla_response_due_at" END), "sla_resolution_due_at"), '1970-01-01 00:00:00+00'::timestamptz)), "id") WHERE "deleted_at" IS NULL;
