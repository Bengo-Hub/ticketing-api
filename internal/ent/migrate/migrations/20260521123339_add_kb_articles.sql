-- Modify "tickets" table
ALTER TABLE "tickets" ADD COLUMN "crm_contact_id" uuid NULL, ADD COLUMN "crm_lead_id" uuid NULL;
-- Create index "ticket_crm_contact_id" to table: "tickets"
CREATE INDEX "ticket_crm_contact_id" ON "tickets" ("crm_contact_id");
-- Create index "ticket_crm_lead_id" to table: "tickets"
CREATE INDEX "ticket_crm_lead_id" ON "tickets" ("crm_lead_id");
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
