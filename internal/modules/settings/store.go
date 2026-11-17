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

	"github.com/google/uuid"
)

// Store reads and writes settings sections.
type Store struct {
	db *sql.DB
}

// NewStore creates the store over the service database.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const getSectionSQL = `SELECT settings -> $2::text FROM tenant_settings WHERE tenant_id = $1`

// putSectionSQL writes one section. The || merge replaces that top-level key and keeps the
// others, in one statement, so two sections saved at once never overwrite each other.
const putSectionSQL = `INSERT INTO tenant_settings (id, tenant_id, settings, updated_at)
VALUES ($1, $2, jsonb_build_object($3::text, $4::jsonb), now())
ON CONFLICT (tenant_id) DO UPDATE
SET settings = COALESCE(tenant_settings.settings, '{}'::jsonb) || EXCLUDED.settings, updated_at = now()`

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

// PutSection saves the named section, replacing what it held before.
func (s *Store) PutSection(ctx context.Context, tenantID uuid.UUID, name string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s settings: %w", name, err)
	}
	if _, err := s.db.ExecContext(ctx, putSectionSQL, uuid.New(), tenantID, name, string(body)); err != nil {
		return fmt.Errorf("save %s settings: %w", name, err)
	}
	return nil
}
