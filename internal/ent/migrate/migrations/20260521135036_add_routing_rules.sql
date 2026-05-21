-- Create "routing_rules" table
CREATE TABLE "routing_rules" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "name" character varying NOT NULL, "description" text NULL, "is_active" boolean NOT NULL DEFAULT true, "sort_order" bigint NOT NULL DEFAULT 0, "conditions" jsonb NOT NULL, "action" jsonb NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "routingrule_tenant_id_is_active" to table: "routing_rules"
CREATE INDEX "routingrule_tenant_id_is_active" ON "routing_rules" ("tenant_id", "is_active");
-- Create index "routingrule_tenant_id_sort_order" to table: "routing_rules"
CREATE INDEX "routingrule_tenant_id_sort_order" ON "routing_rules" ("tenant_id", "sort_order");
