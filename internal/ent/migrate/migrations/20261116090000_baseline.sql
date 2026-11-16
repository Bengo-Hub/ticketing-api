-- Baseline: the schema ent auto-migrate built from internal/ent/schema at commit c0d863b.
-- A database that already has the "tickets" table records this version as applied without
-- running it (cmd/migrate); a fresh database runs it.
-- Create "kb_articles" table
CREATE TABLE "kb_articles" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "author_id" uuid NOT NULL, "title" character varying NOT NULL, "slug" character varying NOT NULL, "content" text NOT NULL, "excerpt" character varying NULL, "category" character varying NULL, "tags" jsonb NULL, "status" character varying NOT NULL DEFAULT 'draft', "is_public" boolean NOT NULL DEFAULT true, "view_count" bigint NOT NULL DEFAULT 0, "helpful_count" bigint NOT NULL DEFAULT 0, "not_helpful_count" bigint NOT NULL DEFAULT 0, "metadata" jsonb NULL, "published_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "deleted_at" timestamptz NULL, PRIMARY KEY ("id"));
-- Create index "kbarticle_author_id" to table: "kb_articles"
CREATE INDEX "kbarticle_author_id" ON "kb_articles" ("author_id");
-- Create index "kbarticle_created_at" to table: "kb_articles"
CREATE INDEX "kbarticle_created_at" ON "kb_articles" ("created_at");
-- Create index "kbarticle_deleted_at" to table: "kb_articles"
CREATE INDEX "kbarticle_deleted_at" ON "kb_articles" ("deleted_at");
-- Create index "kbarticle_tenant_id_category" to table: "kb_articles"
CREATE INDEX "kbarticle_tenant_id_category" ON "kb_articles" ("tenant_id", "category");
-- Create index "kbarticle_tenant_id_is_public" to table: "kb_articles"
CREATE INDEX "kbarticle_tenant_id_is_public" ON "kb_articles" ("tenant_id", "is_public");
-- Create index "kbarticle_tenant_id_slug" to table: "kb_articles"
CREATE UNIQUE INDEX "kbarticle_tenant_id_slug" ON "kb_articles" ("tenant_id", "slug");
-- Create index "kbarticle_tenant_id_status" to table: "kb_articles"
CREATE INDEX "kbarticle_tenant_id_status" ON "kb_articles" ("tenant_id", "status");
-- Create "outbox_events" table
CREATE TABLE "outbox_events" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "aggregate_type" character varying NOT NULL, "aggregate_id" uuid NOT NULL, "event_type" character varying NOT NULL, "payload" bytea NOT NULL, "status" character varying NOT NULL DEFAULT 'PENDING', "attempts" bigint NOT NULL DEFAULT 0, "last_attempt_at" timestamptz NULL, "published_at" timestamptz NULL, "error_message" character varying NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "outboxevent_status_created_at" to table: "outbox_events"
CREATE INDEX "outboxevent_status_created_at" ON "outbox_events" ("status", "created_at");
-- Create index "outboxevent_tenant_id" to table: "outbox_events"
CREATE INDEX "outboxevent_tenant_id" ON "outbox_events" ("tenant_id");
-- Create "routing_rules" table
CREATE TABLE "routing_rules" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "name" character varying NOT NULL, "description" text NULL, "is_active" boolean NOT NULL DEFAULT true, "sort_order" bigint NOT NULL DEFAULT 0, "conditions" jsonb NOT NULL, "action" jsonb NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "routingrule_tenant_id_is_active" to table: "routing_rules"
CREATE INDEX "routingrule_tenant_id_is_active" ON "routing_rules" ("tenant_id", "is_active");
-- Create index "routingrule_tenant_id_sort_order" to table: "routing_rules"
CREATE INDEX "routingrule_tenant_id_sort_order" ON "routing_rules" ("tenant_id", "sort_order");
-- Create "sla_policies" table
CREATE TABLE "sla_policies" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "name" character varying NOT NULL, "description" text NULL, "priority" character varying NOT NULL, "category_id" uuid NULL, "first_response_minutes" bigint NOT NULL, "first_response_business_hours" boolean NOT NULL DEFAULT true, "resolution_minutes" bigint NOT NULL, "resolution_business_hours" boolean NOT NULL DEFAULT true, "is_active" boolean NOT NULL DEFAULT true, "sort_order" bigint NOT NULL DEFAULT 0, "business_hours" jsonb NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "slapolicy_sort_order" to table: "sla_policies"
CREATE INDEX "slapolicy_sort_order" ON "sla_policies" ("sort_order");
-- Create index "slapolicy_tenant_id_category_id" to table: "sla_policies"
CREATE INDEX "slapolicy_tenant_id_category_id" ON "sla_policies" ("tenant_id", "category_id");
-- Create index "slapolicy_tenant_id_is_active" to table: "sla_policies"
CREATE INDEX "slapolicy_tenant_id_is_active" ON "sla_policies" ("tenant_id", "is_active");
-- Create index "slapolicy_tenant_id_priority" to table: "sla_policies"
CREATE INDEX "slapolicy_tenant_id_priority" ON "sla_policies" ("tenant_id", "priority");
-- Create "teams" table
CREATE TABLE "teams" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "name" character varying NOT NULL, "description" text NULL, "is_active" boolean NOT NULL DEFAULT true, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "deleted_at" timestamptz NULL, PRIMARY KEY ("id"));
-- Create index "team_deleted_at" to table: "teams"
CREATE INDEX "team_deleted_at" ON "teams" ("deleted_at");
-- Create index "team_tenant_id_is_active" to table: "teams"
CREATE INDEX "team_tenant_id_is_active" ON "teams" ("tenant_id", "is_active");
-- Create index "team_tenant_id_name" to table: "teams"
CREATE UNIQUE INDEX "team_tenant_id_name" ON "teams" ("tenant_id", "name");
-- Create "agents" table
CREATE TABLE "agents" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "user_id" uuid NOT NULL, "display_name" character varying NOT NULL, "email" character varying NOT NULL, "avatar_url" character varying NULL, "role" character varying NOT NULL DEFAULT 'agent', "status" character varying NOT NULL DEFAULT 'offline', "max_concurrent_tickets" bigint NOT NULL DEFAULT 10, "current_ticket_count" bigint NOT NULL DEFAULT 0, "skills" jsonb NULL, "metadata" jsonb NULL, "is_active" boolean NOT NULL DEFAULT true, "last_active_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "team_id" uuid NULL, PRIMARY KEY ("id"), CONSTRAINT "agents_teams_agents" FOREIGN KEY ("team_id") REFERENCES "teams" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create index "agent_email" to table: "agents"
CREATE INDEX "agent_email" ON "agents" ("email");
-- Create index "agent_team_id" to table: "agents"
CREATE INDEX "agent_team_id" ON "agents" ("team_id");
-- Create index "agent_tenant_id_is_active" to table: "agents"
CREATE INDEX "agent_tenant_id_is_active" ON "agents" ("tenant_id", "is_active");
-- Create index "agent_tenant_id_status" to table: "agents"
CREATE INDEX "agent_tenant_id_status" ON "agents" ("tenant_id", "status");
-- Create index "agent_tenant_id_user_id" to table: "agents"
CREATE UNIQUE INDEX "agent_tenant_id_user_id" ON "agents" ("tenant_id", "user_id");
-- Create index "agents_user_id_key" to table: "agents"
CREATE UNIQUE INDEX "agents_user_id_key" ON "agents" ("user_id");
-- Create "ticket_categories" table
CREATE TABLE "ticket_categories" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "name" character varying NOT NULL, "slug" character varying NOT NULL, "description" text NULL, "icon" character varying NULL, "sort_order" bigint NOT NULL DEFAULT 0, "is_active" boolean NOT NULL DEFAULT true, "default_assignee_id" uuid NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "parent_id" uuid NULL, PRIMARY KEY ("id"), CONSTRAINT "ticket_categories_ticket_categories_children" FOREIGN KEY ("parent_id") REFERENCES "ticket_categories" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create index "ticketcategory_parent_id" to table: "ticket_categories"
CREATE INDEX "ticketcategory_parent_id" ON "ticket_categories" ("parent_id");
-- Create index "ticketcategory_sort_order" to table: "ticket_categories"
CREATE INDEX "ticketcategory_sort_order" ON "ticket_categories" ("sort_order");
-- Create index "ticketcategory_tenant_id_is_active" to table: "ticket_categories"
CREATE INDEX "ticketcategory_tenant_id_is_active" ON "ticket_categories" ("tenant_id", "is_active");
-- Create index "ticketcategory_tenant_id_slug" to table: "ticket_categories"
CREATE UNIQUE INDEX "ticketcategory_tenant_id_slug" ON "ticket_categories" ("tenant_id", "slug");
-- Create "tickets" table
CREATE TABLE "tickets" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "ticket_number" character varying NOT NULL, "customer_id" uuid NOT NULL, "assigned_team_id" uuid NULL, "subject" character varying NOT NULL, "description" text NOT NULL, "status" character varying NOT NULL DEFAULT 'new', "priority" character varying NOT NULL DEFAULT 'normal', "channel" character varying NOT NULL DEFAULT 'web', "order_id" uuid NULL, "external_reference" character varying NULL, "crm_contact_id" uuid NULL, "crm_lead_id" uuid NULL, "sla_response_due_at" timestamptz NULL, "sla_resolution_due_at" timestamptz NULL, "sla_response_breached" boolean NOT NULL DEFAULT false, "sla_resolution_breached" boolean NOT NULL DEFAULT false, "first_response_at" timestamptz NULL, "resolved_at" timestamptz NULL, "closed_at" timestamptz NULL, "reopened_at" timestamptz NULL, "escalation_level" bigint NOT NULL DEFAULT 0, "escalated_at" timestamptz NULL, "tags" jsonb NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "deleted_at" timestamptz NULL, "assigned_agent_id" uuid NULL, "category_id" uuid NULL, PRIMARY KEY ("id"), CONSTRAINT "tickets_agents_assigned_tickets" FOREIGN KEY ("assigned_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT "tickets_ticket_categories_tickets" FOREIGN KEY ("category_id") REFERENCES "ticket_categories" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create index "ticket_category_id" to table: "tickets"
CREATE INDEX "ticket_category_id" ON "tickets" ("category_id");
-- Create index "ticket_created_at" to table: "tickets"
CREATE INDEX "ticket_created_at" ON "tickets" ("created_at");
-- Create index "ticket_crm_contact_id" to table: "tickets"
CREATE INDEX "ticket_crm_contact_id" ON "tickets" ("crm_contact_id");
-- Create index "ticket_crm_lead_id" to table: "tickets"
CREATE INDEX "ticket_crm_lead_id" ON "tickets" ("crm_lead_id");
-- Create index "ticket_deleted_at" to table: "tickets"
CREATE INDEX "ticket_deleted_at" ON "tickets" ("deleted_at");
-- Create index "ticket_external_reference" to table: "tickets"
CREATE INDEX "ticket_external_reference" ON "tickets" ("external_reference");
-- Create index "ticket_order_id" to table: "tickets"
CREATE INDEX "ticket_order_id" ON "tickets" ("order_id");
-- Create index "ticket_sla_resolution_due_at" to table: "tickets"
CREATE INDEX "ticket_sla_resolution_due_at" ON "tickets" ("sla_resolution_due_at");
-- Create index "ticket_sla_response_due_at" to table: "tickets"
CREATE INDEX "ticket_sla_response_due_at" ON "tickets" ("sla_response_due_at");
-- Create index "ticket_tenant_id_assigned_agent_id" to table: "tickets"
CREATE INDEX "ticket_tenant_id_assigned_agent_id" ON "tickets" ("tenant_id", "assigned_agent_id");
-- Create index "ticket_tenant_id_assigned_team_id" to table: "tickets"
CREATE INDEX "ticket_tenant_id_assigned_team_id" ON "tickets" ("tenant_id", "assigned_team_id");
-- Create index "ticket_tenant_id_customer_id" to table: "tickets"
CREATE INDEX "ticket_tenant_id_customer_id" ON "tickets" ("tenant_id", "customer_id");
-- Create index "ticket_tenant_id_priority" to table: "tickets"
CREATE INDEX "ticket_tenant_id_priority" ON "tickets" ("tenant_id", "priority");
-- Create index "ticket_tenant_id_status" to table: "tickets"
CREATE INDEX "ticket_tenant_id_status" ON "tickets" ("tenant_id", "status");
-- Create index "tickets_ticket_number_key" to table: "tickets"
CREATE UNIQUE INDEX "tickets_ticket_number_key" ON "tickets" ("ticket_number");
-- Create "ticket_attachments" table
CREATE TABLE "ticket_attachments" ("id" uuid NOT NULL, "comment_id" uuid NULL, "uploaded_by" uuid NOT NULL, "filename" character varying NOT NULL, "storage_key" character varying NOT NULL, "content_type" character varying NOT NULL, "file_size" bigint NOT NULL, "url" character varying NULL, "url_expires_at" timestamptz NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "deleted_at" timestamptz NULL, "ticket_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "ticket_attachments_tickets_attachments" FOREIGN KEY ("ticket_id") REFERENCES "tickets" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "ticketattachment_comment_id" to table: "ticket_attachments"
CREATE INDEX "ticketattachment_comment_id" ON "ticket_attachments" ("comment_id");
-- Create index "ticketattachment_deleted_at" to table: "ticket_attachments"
CREATE INDEX "ticketattachment_deleted_at" ON "ticket_attachments" ("deleted_at");
-- Create index "ticketattachment_ticket_id" to table: "ticket_attachments"
CREATE INDEX "ticketattachment_ticket_id" ON "ticket_attachments" ("ticket_id");
-- Create index "ticketattachment_uploaded_by" to table: "ticket_attachments"
CREATE INDEX "ticketattachment_uploaded_by" ON "ticket_attachments" ("uploaded_by");
-- Create "ticket_comments" table
CREATE TABLE "ticket_comments" ("id" uuid NOT NULL, "author_id" uuid NULL, "author_type" character varying NOT NULL DEFAULT 'agent', "comment_type" character varying NOT NULL DEFAULT 'public', "content" text NOT NULL, "content_html" text NULL, "is_edited" boolean NOT NULL DEFAULT false, "edited_at" timestamptz NULL, "mentions" jsonb NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "deleted_at" timestamptz NULL, "ticket_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "ticket_comments_tickets_comments" FOREIGN KEY ("ticket_id") REFERENCES "tickets" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "ticketcomment_author_id" to table: "ticket_comments"
CREATE INDEX "ticketcomment_author_id" ON "ticket_comments" ("author_id");
-- Create index "ticketcomment_deleted_at" to table: "ticket_comments"
CREATE INDEX "ticketcomment_deleted_at" ON "ticket_comments" ("deleted_at");
-- Create index "ticketcomment_ticket_id_comment_type" to table: "ticket_comments"
CREATE INDEX "ticketcomment_ticket_id_comment_type" ON "ticket_comments" ("ticket_id", "comment_type");
-- Create index "ticketcomment_ticket_id_created_at" to table: "ticket_comments"
CREATE INDEX "ticketcomment_ticket_id_created_at" ON "ticket_comments" ("ticket_id", "created_at");
