package migrate

import (
	"embed"
	"fmt"
	"io/fs"
	"log"

	atlasmigrate "ariga.io/atlas/sql/migrate"
)

//go:embed migrations/*.sql migrations/atlas.sum
var migrations embed.FS

// Dir is the versioned migration directory cmd/migrate applies. A local checkout uses the files on
// disk; the container, which has no source tree, uses the copy embedded in the binary.
var Dir atlasmigrate.Dir

func init() {
	var err error
	if Dir, err = atlasmigrate.NewLocalDir("internal/ent/migrate/migrations"); err == nil {
		return
	}
	if Dir, err = newEmbedDir(migrations, "migrations"); err != nil {
		log.Fatalf("migrate: load embedded migrations: %v", err)
	}
}

// newEmbedDir copies the embedded migration files into an in-memory atlas directory.
func newEmbedDir(fsys embed.FS, path string) (*atlasmigrate.MemDir, error) {
	sub, err := fs.Sub(fsys, path)
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, err
	}
	md := atlasmigrate.OpenMemDir(path)
	for _, e := range entries {
		data, err := fs.ReadFile(sub, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if err := md.WriteFile(e.Name(), data); err != nil {
			return nil, fmt.Errorf("write %s: %w", e.Name(), err)
		}
	}
	return md, nil
}
