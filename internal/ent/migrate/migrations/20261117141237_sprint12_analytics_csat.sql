-- Create index "ticket_open_backlog" to table: "tickets"
CREATE INDEX "ticket_open_backlog" ON "tickets" ("tenant_id", "status", "assigned_team_id", "assigned_agent_id", "category_id", "channel", "priority", "sla_resolution_due_at", "sla_paused_at") WHERE ((deleted_at IS NULL) AND ((status)::text = ANY ('{new,open,in_progress,pending_customer,pending_internal}'::text[])));
-- Create index "ticket_tenant_id_first_response_at" to table: "tickets"
CREATE INDEX "ticket_tenant_id_first_response_at" ON "tickets" ("tenant_id", "first_response_at");
-- Create index "ticket_tenant_id_sla_resolution_due_at" to table: "tickets"
CREATE INDEX "ticket_tenant_id_sla_resolution_due_at" ON "tickets" ("tenant_id", "sla_resolution_due_at");
-- Create index "ticket_tenant_id_sla_response_due_at" to table: "tickets"
CREATE INDEX "ticket_tenant_id_sla_response_due_at" ON "tickets" ("tenant_id", "sla_response_due_at");
-- Create index "ticket_updated_at_id" to table: "tickets"
CREATE INDEX "ticket_updated_at_id" ON "tickets" ("updated_at", "id");
-- Create "csat_responses" table
CREATE TABLE "csat_responses" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "ticket_id" uuid NOT NULL, "score" bigint NOT NULL, "comment" text NULL, "token_hash" character varying NOT NULL, "submitted_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "csat_responses_token_hash_key" to table: "csat_responses"
CREATE UNIQUE INDEX "csat_responses_token_hash_key" ON "csat_responses" ("token_hash");
-- Create index "csatresponse_tenant_id_submitted_at" to table: "csat_responses"
CREATE INDEX "csatresponse_tenant_id_submitted_at" ON "csat_responses" ("tenant_id", "submitted_at");
-- Create index "csatresponse_ticket_id_submitted_at" to table: "csat_responses"
CREATE INDEX "csatresponse_ticket_id_submitted_at" ON "csat_responses" ("ticket_id", "submitted_at");
-- Create "rollup_watermarks" table
CREATE TABLE "rollup_watermarks" ("id" uuid NOT NULL, "name" character varying NOT NULL, "watermark_at" timestamptz NOT NULL, "watermark_id" uuid NOT NULL, "caught_up_at" timestamptz NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "rollup_watermarks_name_key" to table: "rollup_watermarks"
CREATE UNIQUE INDEX "rollup_watermarks_name_key" ON "rollup_watermarks" ("name");
-- Create "ticket_rollups" table
CREATE TABLE "ticket_rollups" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "granularity" character varying NOT NULL, "bucket_start" timestamptz NOT NULL, "team_id" uuid NOT NULL, "agent_id" uuid NOT NULL, "category_id" uuid NOT NULL, "channel" character varying NOT NULL, "priority" character varying NOT NULL, "created_count" bigint NOT NULL DEFAULT 0, "resolved_count" bigint NOT NULL DEFAULT 0, "first_response_count" bigint NOT NULL DEFAULT 0, "first_response_minutes" bigint NOT NULL DEFAULT 0, "resolution_minutes" bigint NOT NULL DEFAULT 0, "fr_met" bigint NOT NULL DEFAULT 0, "fr_breached" bigint NOT NULL DEFAULT 0, "res_met" bigint NOT NULL DEFAULT 0, "res_breached" bigint NOT NULL DEFAULT 0, "csat_1" bigint NOT NULL DEFAULT 0, "csat_2" bigint NOT NULL DEFAULT 0, "csat_3" bigint NOT NULL DEFAULT 0, "csat_4" bigint NOT NULL DEFAULT 0, "csat_5" bigint NOT NULL DEFAULT 0, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ticketrollup_agent" to table: "ticket_rollups"
CREATE INDEX "ticketrollup_agent" ON "ticket_rollups" ("tenant_id", "granularity", "agent_id", "bucket_start");
-- Create index "ticketrollup_category" to table: "ticket_rollups"
CREATE INDEX "ticketrollup_category" ON "ticket_rollups" ("tenant_id", "granularity", "category_id", "bucket_start");
-- Create index "ticketrollup_cell" to table: "ticket_rollups"
CREATE UNIQUE INDEX "ticketrollup_cell" ON "ticket_rollups" ("tenant_id", "granularity", "bucket_start", "team_id", "agent_id", "category_id", "channel", "priority");
-- Create index "ticketrollup_channel" to table: "ticket_rollups"
CREATE INDEX "ticketrollup_channel" ON "ticket_rollups" ("tenant_id", "granularity", "channel", "bucket_start");
-- Create index "ticketrollup_priority" to table: "ticket_rollups"
CREATE INDEX "ticketrollup_priority" ON "ticket_rollups" ("tenant_id", "granularity", "priority", "bucket_start");
-- Create index "ticketrollup_team" to table: "ticket_rollups"
CREATE INDEX "ticketrollup_team" ON "ticket_rollups" ("tenant_id", "granularity", "team_id", "bucket_start");
-- Create index "ticketrollup_team_category" to table: "ticket_rollups"
CREATE INDEX "ticketrollup_team_category" ON "ticket_rollups" ("tenant_id", "granularity", "team_id", "category_id", "bucket_start");
-- Create index "ticketrollup_team_channel" to table: "ticket_rollups"
CREATE INDEX "ticketrollup_team_channel" ON "ticket_rollups" ("tenant_id", "granularity", "team_id", "channel", "bucket_start");
-- Create index "ticketrollup_team_priority" to table: "ticket_rollups"
CREATE INDEX "ticketrollup_team_priority" ON "ticket_rollups" ("tenant_id", "granularity", "team_id", "priority", "bucket_start");
-- Create "ticket_rollup_dims" table
CREATE TABLE "ticket_rollup_dims" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "granularity" character varying NOT NULL, "bucket_start" timestamptz NOT NULL, "dim_kind" character varying NOT NULL, "team_id" uuid NOT NULL, "dim_value" character varying NOT NULL DEFAULT '', "created_count" bigint NOT NULL DEFAULT 0, "resolved_count" bigint NOT NULL DEFAULT 0, "first_response_count" bigint NOT NULL DEFAULT 0, "first_response_minutes" bigint NOT NULL DEFAULT 0, "resolution_minutes" bigint NOT NULL DEFAULT 0, "fr_met" bigint NOT NULL DEFAULT 0, "fr_breached" bigint NOT NULL DEFAULT 0, "res_met" bigint NOT NULL DEFAULT 0, "res_breached" bigint NOT NULL DEFAULT 0, "csat_1" bigint NOT NULL DEFAULT 0, "csat_2" bigint NOT NULL DEFAULT 0, "csat_3" bigint NOT NULL DEFAULT 0, "csat_4" bigint NOT NULL DEFAULT 0, "csat_5" bigint NOT NULL DEFAULT 0, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ticketrollupdim_bucket" to table: "ticket_rollup_dims"
CREATE INDEX "ticketrollupdim_bucket" ON "ticket_rollup_dims" ("tenant_id", "granularity", "bucket_start");
-- Create index "ticketrollupdim_key" to table: "ticket_rollup_dims"
CREATE UNIQUE INDEX "ticketrollupdim_key" ON "ticket_rollup_dims" ("tenant_id", "granularity", "dim_kind", "team_id", "dim_value", "bucket_start");
-- Create index "ticketrollupdim_kind" to table: "ticket_rollup_dims"
CREATE INDEX "ticketrollupdim_kind" ON "ticket_rollup_dims" ("tenant_id", "granularity", "dim_kind", "bucket_start");
-- Create index "ticketrollupdim_pair" to table: "ticket_rollup_dims"
CREATE INDEX "ticketrollupdim_pair" ON "ticket_rollup_dims" ("tenant_id", "granularity", "dim_kind", "team_id", "bucket_start");
-- Create "ticket_rollup_marks" table
CREATE TABLE "ticket_rollup_marks" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "hours" jsonb NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
