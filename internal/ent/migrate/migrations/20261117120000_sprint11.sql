-- Sprint 11: channel ingest. The per-tenant settings document, the dedupe key of a channel
-- message kept as a comment, and the requester phone lookup behind WhatsApp threading.
-- Create "tenant_settings" table
CREATE TABLE "tenant_settings" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "settings" jsonb NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "tenant_settings_tenant_id_key" to table: "tenant_settings"
CREATE UNIQUE INDEX "tenant_settings_tenant_id_key" ON "tenant_settings" ("tenant_id");
-- An email or WhatsApp message added to a ticket is a comment with metadata channel and
-- external_id; this key stores each message once, so a redelivered message is refused in the
-- comment's own transaction. No existing comment has external_id, so it cannot collide. Ent
-- cannot declare an expression index, so it exists only here; FindCommentByExternalID repeats
-- the expression exactly.
CREATE UNIQUE INDEX "ticketcomment_tenant_channel_external_id" ON "ticket_comments" ("tenant_id", (("metadata" ->> 'channel')), (("metadata" ->> 'external_id'))) WHERE (("metadata" ->> 'external_id') IS NOT NULL);
-- Every inbound WhatsApp message looks up the sender's latest open ticket by phone, so the
-- lookup is indexed. LatestOpenTicketByPhone repeats the expression exactly.
CREATE INDEX "ticket_tenant_requester_phone" ON "tickets" ("tenant_id", (("metadata" ->> 'requester_phone'))) WHERE (("metadata" ->> 'requester_phone') IS NOT NULL);
