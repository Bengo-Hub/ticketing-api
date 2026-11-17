// Package settings keeps ticketing's per-tenant settings: one JSON document per tenant in
// tenant_settings, made of named sections ("channels"). Nothing here is queried by value, so the
// document stays metadata and a new setting needs no migration.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Store reads and writes settings sections.
type Store struct {
	db *sql.DB
}

// NewStore creates the store over the service database.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const getSectionSQL = `SELECT settings -> $2::text FROM tenant_settings WHERE tenant_id = $1`

// putSectionKeySQL writes one key of one section ($3 section, $4 key, $5 value). The || merges
// replace that key and keep every other key and section, in one statement, so two writers of
// different keys never overwrite each other.
const putSectionKeySQL = `INSERT INTO tenant_settings (id, tenant_id, settings, updated_at)
VALUES ($1, $2, jsonb_build_object($3::text, jsonb_build_object($4::text, $5::jsonb)), now())
ON CONFLICT (tenant_id) DO UPDATE
SET settings = COALESCE(tenant_settings.settings, '{}'::jsonb) || jsonb_build_object($3::text,
		COALESCE(tenant_settings.settings -> $3::text, '{}'::jsonb) || jsonb_build_object($4::text, $5::jsonb)),
	updated_at = now()`

// touchSectionTimeSQL sets section.object.key ($3, $4, $5) to the time $6 only when the stored
// time is missing or older than $7, so the comparison and the write are one statement and a row
// written less than a minute ago is left alone.
const touchSectionTimeSQL = `INSERT INTO tenant_settings (id, tenant_id, settings, updated_at)
VALUES ($1, $2, jsonb_build_object($3::text, jsonb_build_object($4::text, jsonb_build_object($5::text, $6::text))), now())
ON CONFLICT (tenant_id) DO UPDATE
SET settings = COALESCE(tenant_settings.settings, '{}'::jsonb) || jsonb_build_object($3::text,
		COALESCE(tenant_settings.settings -> $3::text, '{}'::jsonb) || jsonb_build_object($4::text,
			COALESCE(tenant_settings.settings -> $3::text -> $4::text, '{}'::jsonb) || jsonb_build_object($5::text, $6::text))),
	updated_at = now()
WHERE tenant_settings.settings -> $3::text -> $4::text ->> $5::text IS NULL
	OR (tenant_settings.settings -> $3::text -> $4::text ->> $5::text)::timestamptz < $7::timestamptz`

// Section decodes the named section into into. found is false when the tenant has never saved it.
func (s *Store) Section(ctx context.Context, tenantID uuid.UUID, name string, into any) (bool, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, getSectionSQL, tenantID, name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && len(raw) == 0) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s settings: %w", name, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, fmt.Errorf("decode %s settings: %w", name, err)
	}
	return true, nil
}

// PutSectionKey saves one key of a section, replacing what that key held before.
func (s *Store) PutSectionKey(ctx context.Context, tenantID uuid.UUID, section, key string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s.%s settings: %w", section, key, err)
	}
	if _, err := s.db.ExecContext(ctx, putSectionKeySQL, uuid.New(), tenantID, section, key, string(body)); err != nil {
		return fmt.Errorf("save %s.%s settings: %w", section, key, err)
	}
	return nil
}

// TouchSectionTime stores at (RFC 3339) under section.object.key unless the stored time is less
// than every older than at.
func (s *Store) TouchSectionTime(ctx context.Context, tenantID uuid.UUID, section, object, key string, at time.Time, every time.Duration) error {
	stamp := at.UTC().Format(time.RFC3339)
	floor := at.Add(-every).UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, touchSectionTimeSQL, uuid.New(), tenantID, section, object, key, stamp, floor); err != nil {
		return fmt.Errorf("touch %s.%s.%s settings: %w", section, object, key, err)
	}
	return nil
}
