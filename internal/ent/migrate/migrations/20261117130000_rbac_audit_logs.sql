-- Create "audit_logs" table
CREATE TABLE "audit_logs" ("id" uuid NOT NULL, "tenant_id" uuid NULL, "user_id" uuid NOT NULL, "user_email" character varying NULL, "action" character varying NOT NULL, "resource_type" character varying NOT NULL, "resource_id" character varying NOT NULL, "changes" text NULL, "ip_address" character varying NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "auditlog_tenant_id_created_at" to table: "audit_logs"
CREATE INDEX "auditlog_tenant_id_created_at" ON "audit_logs" ("tenant_id", "created_at");
-- Create index "auditlog_tenant_id_resource_type_resource_id" to table: "audit_logs"
CREATE INDEX "auditlog_tenant_id_resource_type_resource_id" ON "audit_logs" ("tenant_id", "resource_type", "resource_id");
-- Create index "auditlog_user_id" to table: "audit_logs"
CREATE INDEX "auditlog_user_id" ON "audit_logs" ("user_id");
