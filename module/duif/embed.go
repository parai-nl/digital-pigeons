//go:build !dev

package duif

import "embed"

// frontendFS draagt de gebouwde module-frontend; migrationsFS de
// Postgres-migraties. Het dev-buildtag-pair (embed_dev.go) swapt de frontend
// voor os.DirFS zodat tests geen gebouwde bundle nodig hebben.

//go:embed all:frontend/dist
var frontendFS embed.FS

//go:embed migrations
var migrationsFS embed.FS
