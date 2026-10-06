package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
)

// De site-root van deze binary is de parai-core-shell, en die wordt niet
// meegecommit: `make shell-static` kopieert hem uit de core-checkout naar
// static/. Zonder die stap zou de server een lege map serveren. Deze
// placeholder maakt daar een leesbare pagina van die vertelt wat er moet
// gebeuren — en die de module-API en de bundle gewoon laat zien.

//go:embed placeholder/index.html
var placeholderFS embed.FS

// withPlaceholder geeft de site terug zoals hij is zodra er een index.html in
// staat, en anders de ingebakken placeholder. Een andere leesfout dan
// "bestaat niet" is een echte fout: dan is er iets mis met de site-root zelf.
func withPlaceholder(site fs.FS) (fs.FS, error) {
	_, err := fs.Stat(site, "index.html")
	switch {
	case err == nil:
		return site, nil
	case errors.Is(err, fs.ErrNotExist):
		return fs.Sub(placeholderFS, "placeholder")
	default:
		return nil, fmt.Errorf("site-root lezen: %w", err)
	}
}
