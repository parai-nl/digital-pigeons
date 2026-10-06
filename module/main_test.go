package main

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestListenAddr_DefaultsToPort8080(t *testing.T) {
	t.Setenv("DUIF_BIND_HOST", "")
	t.Setenv("PORT", "")
	if got := listenAddr(); got != ":8080" {
		t.Errorf("listenAddr() = %q, want :8080", got)
	}
}

func TestListenAddr_UsesPORT(t *testing.T) {
	t.Setenv("DUIF_BIND_HOST", "")
	t.Setenv("PORT", "9191")
	if got := listenAddr(); got != ":9191" {
		t.Errorf("listenAddr() = %q, want :9191", got)
	}
}

// Een niet-geladen module/.env is lokaal de meest waarschijnlijke bootfout;
// de melding moet daarnaar wijzen in plaats van naar een core-interne
// configfout.
func TestRun_WithoutDatabaseURL_PointsAtDotEnv(t *testing.T) {
	t.Setenv("PARAI_DB_URL", "")

	err := run()
	if err == nil {
		t.Fatal("run() zonder PARAI_DB_URL moet een fout geven")
	}
	if !strings.Contains(err.Error(), "PARAI_DB_URL") || !strings.Contains(err.Error(), ".env") {
		t.Errorf("foutmelding = %q; moet PARAI_DB_URL én .env noemen", err)
	}
}

// De shell-statics komen uit dezelfde bron als waar de binary ze uit serveert;
// staat de shell er niet in, dan valt de site terug op de placeholder. Beide
// takken leveren dus altijd een index.html.
func TestStaticSite_AlwaysServesAnIndex(t *testing.T) {
	site, err := staticSite()
	if err != nil {
		t.Fatalf("staticSite: %v", err)
	}
	if _, err := fs.Stat(site, "index.html"); err != nil {
		t.Errorf("index.html niet bereikbaar via staticSite(): %v", err)
	}
}

// Staat de gebouwde shell in static/, dan serveert de binary die — de
// placeholder blijft dan volledig buiten beeld.
func TestWithPlaceholder_KeepsTheShellWhenItIsThere(t *testing.T) {
	shell := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>de echte shell</html>")}}
	site, err := withPlaceholder(shell)
	if err != nil {
		t.Fatalf("withPlaceholder: %v", err)
	}
	raw, err := fs.ReadFile(site, "index.html")
	if err != nil {
		t.Fatalf("index.html lezen: %v", err)
	}
	if !strings.Contains(string(raw), "de echte shell") {
		t.Errorf("de shell werd niet geserveerd: %s", raw)
	}
	if strings.Contains(string(raw), "duif-placeholder") {
		t.Error("de placeholder overschreef de echte shell")
	}
}

// Zonder shell serveert de binary een pagina die vertelt wat er moet gebeuren,
// met de module-API en de bundle als werkende verwijzingen.
func TestWithPlaceholder_FallsBackToTheEmbeddedPage(t *testing.T) {
	site, err := withPlaceholder(fstest.MapFS{})
	if err != nil {
		t.Fatalf("withPlaceholder: %v", err)
	}
	raw, err := fs.ReadFile(site, "index.html")
	if err != nil {
		t.Fatalf("placeholder lezen: %v", err)
	}
	for _, want := range []string{
		`name="duif-placeholder"`,
		"make shell-static",
		"/api/duif/v1/health",
		"/modules/duif/bundle.js",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("de placeholder mist %q:\n%s", want, raw)
		}
	}
}

// Een site-root die niet te lezen is, is geen "shell ontbreekt": die fout gaat
// omhoog in plaats van stil een placeholder op te leveren.
func TestWithPlaceholder_ReportsOtherErrors(t *testing.T) {
	if _, err := withPlaceholder(brokenFS{}); err == nil {
		t.Fatal("een onleesbare site-root hoort een fout te geven")
	}
}

// brokenFS is een site-root waar elke leespoging op stukloopt.
type brokenFS struct{}

func (brokenFS) Open(string) (fs.File, error) { return nil, fs.ErrPermission }

func TestListenAddr_BindHostKeepsServerOnLoopback(t *testing.T) {
	t.Setenv("DUIF_BIND_HOST", "127.0.0.1")
	t.Setenv("PORT", "8081")
	if got := listenAddr(); got != "127.0.0.1:8081" {
		t.Fatalf("bind = %s", got)
	}
}
