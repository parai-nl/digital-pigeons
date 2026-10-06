//go:build dev

package main

import (
	"io/fs"
	"os"
)

// staticSite leest in dev-modus live van schijf, zodat wijzigingen aan de
// site zonder rebuild zichtbaar zijn.
func staticSite() (fs.FS, error) {
	return withPlaceholder(os.DirFS("static"))
}
