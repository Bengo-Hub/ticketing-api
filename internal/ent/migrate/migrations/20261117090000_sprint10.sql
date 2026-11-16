-- Sprint 10: business calendars, policy calendars in place of the unused business-hours flags,
-- and the SLA pause, policy and elapsed-minutes columns on tickets.
-- Modify "sla_policies" table
ALTER TABLE "sla_policies" DROP COLUMN "first_response_business_hours", DROP COLUMN "resolution_business_hours", DROP COLUMN "business_hours", ADD COLUMN "calendar_id" uuid NULL;
-- Create index "slapolicy_tenant_id_calendar_id" to table: "sla_policies"
CREATE INDEX "slapolicy_tenant_id_calendar_id" ON "sla_policies" ("tenant_id", "calendar_id");
-- Modify "tickets" table
ALTER TABLE "tickets" ADD COLUMN "sla_policy_id" uuid NULL, ADD COLUMN "sla_paused_at" timestamptz NULL, ADD COLUMN "sla_paused_minutes" bigint NOT NULL DEFAULT 0, ADD COLUMN "sla_first_response_minutes" bigint NULL, ADD COLUMN "sla_resolution_minutes" bigint NULL;
-- Create "business_calendars" table
CREATE TABLE "business_calendars" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "name" character varying NOT NULL, "timezone" character varying NOT NULL DEFAULT 'Africa/Nairobi', "weekly_hours" jsonb NOT NULL, "holidays" jsonb NULL, "is_default" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "businesscalendar_tenant_id_name" to table: "business_calendars"
CREATE UNIQUE INDEX "businesscalendar_tenant_id_name" ON "business_calendars" ("tenant_id", "name");
-- Create index "businesscalendar_tenant_id_is_default" to table: "business_calendars"
CREATE INDEX "businesscalendar_tenant_id_is_default" ON "business_calendars" ("tenant_id", "is_default");
-- Open tickets with deadlines get the policy that set them, picked the way ticket create picks
-- it (a category policy first, then the catch-all, lowest sort_order), so the worker can warn and
-- escalate them. Closed history keeps a null policy.
UPDATE "tickets" AS t SET "sla_policy_id" = (SELECT p."id" FROM "sla_policies" AS p WHERE p."tenant_id" = t."tenant_id" AND p."priority" = t."priority" AND p."is_active" AND (p."category_id" = t."category_id" OR p."category_id" IS NULL) ORDER BY (p."category_id" IS NULL), p."sort_order" LIMIT 1) WHERE t."sla_policy_id" IS NULL AND t."deleted_at" IS NULL AND t."status" NOT IN ('resolved', 'closed', 'cancelled') AND (t."sla_response_due_at" IS NOT NULL OR t."sla_resolution_due_at" IS NOT NULL);
