// Command duif is de webserver van de "Digitale Duif van Rotterdam"
// (Parai × AI-MAPS / EUR): een parai-core-app met uitsluitend de duif-module
// geregistreerd: een eigen repository, met de private core als gepinde
// Go-dependency.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	parai "github.com/parai-nl/parai-core"

	"github.com/parai-nl/ai-maps-duiven/module/duif"
)

const defaultPort = "8080"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "duif:", err)
		os.Exit(1)
	}
}

func run() error {
	// Vroege, begrijpelijke melding vóórdat core over de databaseconfig
	// begint: lokaal is een niet-geladen module/.env verreweg de meest
	// waarschijnlijke oorzaak. De URL-vorm zelf valideert core.
	if os.Getenv("PARAI_DB_URL") == "" {
		return errors.New("PARAI_DB_URL is niet gezet — maak module/.env aan (zie .env.example) en laad die vóór het starten")
	}

	site, err := staticSite()
	if err != nil {
		return fmt.Errorf("static assets: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := parai.NewApp(
		parai.WithName("Digitale Duiven"),
		parai.WithStaticFS(site),
	)
	app.Register(duif.New())
	return app.Serve(ctx, listenAddr())
}

// listenAddr bouwt het luisteradres uit PORT (default 8080).
func listenAddr() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	return net.JoinHostPort(os.Getenv("DUIF_BIND_HOST"), port)
}
