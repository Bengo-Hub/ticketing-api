-- Phase 0: per-tenant ticket numbers, internal-create idempotency, tenant_id on comments and
-- attachments, agents unique per tenant, and the local RBAC tables.
-- Ticket numbers are unique within a tenant, not across tenants.
DROP INDEX IF EXISTS "tickets_ticket_number_key";
-- Create index "ticket_tenant_id_ticket_number" to table: "tickets"
CREATE UNIQUE INDEX "ticket_tenant_id_ticket_number" ON "tickets" ("tenant_id", "ticket_number");
-- Internal-create idempotency key: (tenant, metadata source, source_ref.type, source_ref.id). The
-- optional source_ref.parent is context only. Ent cannot declare an expression index, so it exists
-- only here.
CREATE UNIQUE INDEX "ticket_tenant_source_ref" ON "tickets" ("tenant_id", (("metadata" ->> 'source')), (("metadata" -> 'source_ref' ->> 'type')), (("metadata" -> 'source_ref' ->> 'id'))) WHERE (("metadata" -> 'source_ref' ->> 'id') IS NOT NULL);
-- A user may be an agent in more than one tenant; agent_tenant_id_user_id stays.
DROP INDEX IF EXISTS "agents_user_id_key";
-- Comments carry their ticket's tenant: add nullable, backfill from the ticket, then require it.
ALTER TABLE "ticket_comments" ADD COLUMN "tenant_id" uuid NULL;
UPDATE "ticket_comments" AS c SET "tenant_id" = t."tenant_id" FROM "tickets" AS t WHERE c."ticket_id" = t."id" AND c."tenant_id" IS NULL;
ALTER TABLE "ticket_comments" ALTER COLUMN "tenant_id" SET NOT NULL;
-- Create index "ticketcomment_tenant_id_ticket_id" to table: "ticket_comments"
CREATE INDEX "ticketcomment_tenant_id_ticket_id" ON "ticket_comments" ("tenant_id", "ticket_id");
-- Attachments carry their ticket's tenant, filled the same way.
ALTER TABLE "ticket_attachments" ADD COLUMN "tenant_id" uuid NULL;
UPDATE "ticket_attachments" AS a SET "tenant_id" = t."tenant_id" FROM "tickets" AS t WHERE a."ticket_id" = t."id" AND a."tenant_id" IS NULL;
ALTER TABLE "ticket_attachments" ALTER COLUMN "tenant_id" SET NOT NULL;
-- Create index "ticketattachment_tenant_id_ticket_id" to table: "ticket_attachments"
CREATE INDEX "ticketattachment_tenant_id_ticket_id" ON "ticket_attachments" ("tenant_id", "ticket_id");
-- Create "ticket_sequences" table
CREATE TABLE "ticket_sequences" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "next_value" bigint NOT NULL DEFAULT 0, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ticket_sequences_tenant_id_key" to table: "ticket_sequences"
CREATE UNIQUE INDEX "ticket_sequences_tenant_id_key" ON "ticket_sequences" ("tenant_id");
-- Each tenant's counter starts at the highest TKT-number it already has (soft-deleted included).
INSERT INTO "ticket_sequences" ("id", "tenant_id", "next_value", "updated_at") SELECT gen_random_uuid(), "tenant_id", COALESCE(MAX(CASE WHEN "ticket_number" ~ '^TKT-[0-9]+$' THEN substring("ticket_number" FROM 5)::bigint END), 0), now() FROM "tickets" GROUP BY "tenant_id";
-- Create "ticketing_permissions" table
CREATE TABLE "ticketing_permissions" ("id" uuid NOT NULL, "permission_code" character varying NOT NULL, "module" character varying NOT NULL, "action" character varying NOT NULL, "resource" character varying NULL, "description" text NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ticketing_permissions_permission_code_key" to table: "ticketing_permissions"
CREATE UNIQUE INDEX "ticketing_permissions_permission_code_key" ON "ticketing_permissions" ("permission_code");
-- Create index "ticketingpermission_module_action" to table: "ticketing_permissions"
CREATE INDEX "ticketingpermission_module_action" ON "ticketing_permissions" ("module", "action");
-- Create "ticketing_roles" table
CREATE TABLE "ticketing_roles" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "role_code" character varying NOT NULL, "name" character varying NOT NULL, "description" text NULL, "is_system_role" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ticketingrole_tenant_id_role_code" to table: "ticketing_roles"
CREATE UNIQUE INDEX "ticketingrole_tenant_id_role_code" ON "ticketing_roles" ("tenant_id", "role_code");
-- Create "ticketing_users" table
CREATE TABLE "ticketing_users" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "auth_service_user_id" uuid NOT NULL, "email" character varying NULL, "name" character varying NULL, "sync_status" character varying NOT NULL DEFAULT 'synced', "last_sync_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ticketinguser_auth_service_user_id" to table: "ticketing_users"
CREATE INDEX "ticketinguser_auth_service_user_id" ON "ticketing_users" ("auth_service_user_id");
-- Create index "ticketinguser_tenant_id_auth_service_user_id" to table: "ticketing_users"
CREATE UNIQUE INDEX "ticketinguser_tenant_id_auth_service_user_id" ON "ticketing_users" ("tenant_id", "auth_service_user_id");
-- Create "role_permissions" table
CREATE TABLE "role_permissions" ("id" bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY, "role_id" uuid NOT NULL, "permission_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "role_permissions_ticketing_permissions_permission" FOREIGN KEY ("permission_id") REFERENCES "ticketing_permissions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT "role_permissions_ticketing_roles_role" FOREIGN KEY ("role_id") REFERENCES "ticketing_roles" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- Create index "rolepermission_permission_id" to table: "role_permissions"
CREATE INDEX "rolepermission_permission_id" ON "role_permissions" ("permission_id");
-- Create index "rolepermission_role_id_permission_id" to table: "role_permissions"
CREATE UNIQUE INDEX "rolepermission_role_id_permission_id" ON "role_permissions" ("role_id", "permission_id");
-- Create "user_role_assignments" table
CREATE TABLE "user_role_assignments" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "assigned_by" uuid NOT NULL, "assigned_at" timestamptz NOT NULL, "expires_at" timestamptz NULL, "role_id" uuid NOT NULL, "user_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "user_role_assignments_ticketing_roles_role" FOREIGN KEY ("role_id") REFERENCES "ticketing_roles" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT "user_role_assignments_ticketing_users_user" FOREIGN KEY ("user_id") REFERENCES "ticketing_users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- Create index "userroleassignment_role_id" to table: "user_role_assignments"
CREATE INDEX "userroleassignment_role_id" ON "user_role_assignments" ("role_id");
-- Create index "userroleassignment_tenant_id_user_id_role_id" to table: "user_role_assignments"
CREATE UNIQUE INDEX "userroleassignment_tenant_id_user_id_role_id" ON "user_role_assignments" ("tenant_id", "user_id", "role_id");
