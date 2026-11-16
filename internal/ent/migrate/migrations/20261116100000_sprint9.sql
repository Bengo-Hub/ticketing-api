-- Sprint 9: keyset and filter indexes on tickets, trigram search on tickets and the knowledge base,
-- tag containment, and the saved views, macros and ticket links tables.
-- pg_trgm is installed in production already; IF NOT EXISTS keeps a fresh database working.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- Create index "ticket_tenant_id_created_at_id" to table: "tickets"
CREATE INDEX "ticket_tenant_id_created_at_id" ON "tickets" ("tenant_id", "created_at", "id");
-- Create index "ticket_tenant_id_channel" to table: "tickets"
CREATE INDEX "ticket_tenant_id_channel" ON "tickets" ("tenant_id", "channel");
-- Create index "ticket_tenant_id_category_id" to table: "tickets"
CREATE INDEX "ticket_tenant_id_category_id" ON "tickets" ("tenant_id", "category_id");
-- Create index "ticket_tenant_id_resolved_at" to table: "tickets"
CREATE INDEX "ticket_tenant_id_resolved_at" ON "tickets" ("tenant_id", "resolved_at");
-- Create index "ticket_subject_trgm" to table: "tickets"
CREATE INDEX "ticket_subject_trgm" ON "tickets" USING GIN ("subject" gin_trgm_ops);
-- Create index "ticket_description_trgm" to table: "tickets"
CREATE INDEX "ticket_description_trgm" ON "tickets" USING GIN ("description" gin_trgm_ops);
-- Create index "ticket_tags_gin" to table: "tickets"
CREATE INDEX "ticket_tags_gin" ON "tickets" USING GIN ("tags");
-- Requester search over the name and email kept in metadata. Ent cannot declare an expression
-- index, so it exists only here; the list query repeats this expression exactly.
CREATE INDEX "ticket_requester_trgm" ON "tickets" USING GIN ((COALESCE("metadata" ->> 'requester_email', '') || ' ' || COALESCE("metadata" ->> 'requester_name', '')) gin_trgm_ops);
-- Create index "kbarticle_title_trgm" to table: "kb_articles"
CREATE INDEX "kbarticle_title_trgm" ON "kb_articles" USING GIN ("title" gin_trgm_ops);
-- Create index "kbarticle_excerpt_trgm" to table: "kb_articles"
CREATE INDEX "kbarticle_excerpt_trgm" ON "kb_articles" USING GIN ("excerpt" gin_trgm_ops);
-- Create index "kbarticle_content_trgm" to table: "kb_articles"
CREATE INDEX "kbarticle_content_trgm" ON "kb_articles" USING GIN ("content" gin_trgm_ops);
-- Create "ticket_views" table
CREATE TABLE "ticket_views" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "owner_user_id" uuid NOT NULL, "name" character varying NOT NULL, "filters" jsonb NULL, "sort" character varying NOT NULL DEFAULT 'created_desc', "shared" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ticketview_tenant_id_owner_user_id" to table: "ticket_views"
CREATE INDEX "ticketview_tenant_id_owner_user_id" ON "ticket_views" ("tenant_id", "owner_user_id");
-- Create index "ticketview_tenant_id_shared" to table: "ticket_views"
CREATE INDEX "ticketview_tenant_id_shared" ON "ticket_views" ("tenant_id", "shared");
-- Create "macros" table
CREATE TABLE "macros" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "name" character varying NOT NULL, "body" text NULL, "actions" jsonb NULL, "is_active" boolean NOT NULL DEFAULT true, "created_by" uuid NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "macro_tenant_id_name" to table: "macros"
CREATE UNIQUE INDEX "macro_tenant_id_name" ON "macros" ("tenant_id", "name");
-- Create "ticket_links" table
CREATE TABLE "ticket_links" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "from_ticket_id" uuid NOT NULL, "to_ticket_id" uuid NOT NULL, "kind" character varying NOT NULL, "created_by" uuid NOT NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ticketlink_tenant_id_from_ticket_id_to_ticket_id_kind" to table: "ticket_links"
CREATE UNIQUE INDEX "ticketlink_tenant_id_from_ticket_id_to_ticket_id_kind" ON "ticket_links" ("tenant_id", "from_ticket_id", "to_ticket_id", "kind");
-- Create index "ticketlink_tenant_id_to_ticket_id" to table: "ticket_links"
CREATE INDEX "ticketlink_tenant_id_to_ticket_id" ON "ticket_links" ("tenant_id", "to_ticket_id");
