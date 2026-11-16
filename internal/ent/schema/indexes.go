package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/index"
)

// trgmIndex is a GIN trigram index on one text column. It serves `col ILIKE '%term%'` for terms of
// three characters or more. It needs the pg_trgm extension, which the sprint 9 migration creates
// when missing; ent does not manage extensions.
func trgmIndex(field, name string) ent.Index {
	return index.Fields(field).
		StorageKey(name).
		Annotations(
			entsql.IndexTypes(map[string]string{dialect.Postgres: "GIN"}),
			entsql.OpClass("gin_trgm_ops"),
		)
}

// ginIndex is a plain GIN index on a jsonb column. It serves containment (`col @> '["x"]'`).
func ginIndex(field, name string) ent.Index {
	return index.Fields(field).
		StorageKey(name).
		Annotations(entsql.IndexTypes(map[string]string{dialect.Postgres: "GIN"}))
}
