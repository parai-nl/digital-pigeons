//go:build !dev

package main

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:static
var embeddedStatic embed.FS

// staticSite levert de site-root zoals die in het binary is gebakken, met de
// placeholder als de shell er niet in zit.
func staticSite() (fs.FS, error) {
	site, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return nil, fmt.Errorf("embedded statics: %w", err)
	}
	return withPlaceholder(site)
}
