package migrate

import (
	"log"

	atlasmigrate "ariga.io/atlas/sql/migrate"
)

// Dir is the migration directory used at runtime to apply versioned migrations.
var Dir atlasmigrate.Dir

func init() {
	var err error
	Dir, err = atlasmigrate.NewLocalDir("internal/ent/migrate/migrations")
	if err != nil {
		log.Printf("Warning: failed to create local migration dir: %v", err)
	}
}
