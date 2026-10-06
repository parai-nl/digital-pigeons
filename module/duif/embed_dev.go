//go:build dev

package duif

import (
	"embed"
	"io/fs"
	"os"
)

// dev-variant: de frontend komt van schijf zodat `go test -tags dev` en
// `make dev` geen embed-refresh nodig hebben.
var frontendFS fs.FS = os.DirFS("duif")

//go:embed migrations
var migrationsFS embed.FS
