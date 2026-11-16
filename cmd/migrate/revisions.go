package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	atlasmigrate "ariga.io/atlas/sql/migrate"
)

// revisionTable records every applied migration file, one row per version.
const revisionTable = "atlas_schema_revisions"

// execQuerier is the part of *sql.DB and *sql.Tx the revision store needs.
type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// revisions is the atlas RevisionReadWriter backed by atlas_schema_revisions in the public schema.
type revisions struct{ q execQuerier }

var _ atlasmigrate.RevisionReadWriter = (*revisions)(nil)

// ensureRevisionTable creates the revision table when it is missing.
func ensureRevisionTable(ctx context.Context, q execQuerier) error {
	_, err := q.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+revisionTable+` (
		version          VARCHAR(255) NOT NULL PRIMARY KEY,
		description      VARCHAR(255) NOT NULL DEFAULT '',
		type             BIGINT       NOT NULL DEFAULT 2,
		applied          BIGINT       NOT NULL DEFAULT 0,
		total            BIGINT       NOT NULL DEFAULT 0,
		executed_at      TIMESTAMPTZ  NOT NULL,
		execution_time   BIGINT       NOT NULL DEFAULT 0,
		error            TEXT         NOT NULL DEFAULT '',
		error_stmt       TEXT         NOT NULL DEFAULT '',
		hash             VARCHAR(255) NOT NULL DEFAULT '',
		partial_hashes   JSONB        NULL,
		operator_version VARCHAR(255) NOT NULL DEFAULT ''
	)`)
	return err
}

// countRevisions returns how many revisions are recorded.
func countRevisions(ctx context.Context, q execQuerier) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+revisionTable).Scan(&n)
	return n, err
}

// tableExists reports whether a table exists in the public schema.
func tableExists(ctx context.Context, q execQuerier, name string) (bool, error) {
	var ok bool
	err := q.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
		name).Scan(&ok)
	return ok, err
}

// Ident names the revision table so atlas leaves it out of its own checks.
func (r *revisions) Ident() *atlasmigrate.TableIdent {
	return &atlasmigrate.TableIdent{Name: revisionTable, Schema: "public"}
}

const revisionColumns = `version, description, type, applied, total, executed_at, execution_time, error, error_stmt, hash, partial_hashes, operator_version`

// ReadRevisions returns every revision ordered by version.
func (r *revisions) ReadRevisions(ctx context.Context) ([]*atlasmigrate.Revision, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+revisionColumns+` FROM `+revisionTable+` ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*atlasmigrate.Revision
	for rows.Next() {
		rev, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

// ReadRevision returns one revision or atlas's ErrRevisionNotExist.
func (r *revisions) ReadRevision(ctx context.Context, version string) (*atlasmigrate.Revision, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+revisionColumns+` FROM `+revisionTable+` WHERE version = $1`, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, atlasmigrate.ErrRevisionNotExist
	}
	return scanRevision(rows)
}

// WriteRevision inserts or replaces a revision.
func (r *revisions) WriteRevision(ctx context.Context, rev *atlasmigrate.Revision) error {
	var partial []byte
	if len(rev.PartialHashes) > 0 {
		b, err := json.Marshal(rev.PartialHashes)
		if err != nil {
			return err
		}
		partial = b
	}
	_, err := r.q.ExecContext(ctx, `INSERT INTO `+revisionTable+` (`+revisionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (version) DO UPDATE SET
			description = EXCLUDED.description, type = EXCLUDED.type, applied = EXCLUDED.applied,
			total = EXCLUDED.total, executed_at = EXCLUDED.executed_at, execution_time = EXCLUDED.execution_time,
			error = EXCLUDED.error, error_stmt = EXCLUDED.error_stmt, hash = EXCLUDED.hash,
			partial_hashes = EXCLUDED.partial_hashes, operator_version = EXCLUDED.operator_version`,
		rev.Version, rev.Description, int64(rev.Type), rev.Applied, rev.Total, rev.ExecutedAt,
		int64(rev.ExecutionTime), rev.Error, rev.ErrorStmt, rev.Hash, nullJSON(partial), rev.OperatorVersion)
	return err
}

// DeleteRevision removes a revision.
func (r *revisions) DeleteRevision(ctx context.Context, version string) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM `+revisionTable+` WHERE version = $1`, version)
	return err
}

func scanRevision(rows *sql.Rows) (*atlasmigrate.Revision, error) {
	var (
		rev      atlasmigrate.Revision
		typ      int64
		execTime int64
		partial  []byte
	)
	if err := rows.Scan(&rev.Version, &rev.Description, &typ, &rev.Applied, &rev.Total, &rev.ExecutedAt,
		&execTime, &rev.Error, &rev.ErrorStmt, &rev.Hash, &partial, &rev.OperatorVersion); err != nil {
		return nil, fmt.Errorf("scan revision: %w", err)
	}
	rev.Type = atlasmigrate.RevisionType(typ)
	rev.ExecutionTime = time.Duration(execTime)
	if len(partial) > 0 {
		if err := json.Unmarshal(partial, &rev.PartialHashes); err != nil {
			return nil, errors.Join(errors.New("decode partial hashes"), err)
		}
	}
	return &rev, nil
}

func nullJSON(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}
