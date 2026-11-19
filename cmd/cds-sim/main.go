// Command cds-sim serves simulated ship data over the OpenRVDAS
// CachedDataServer WebSocket protocol, for the docker compose test
// environment and CI e2e tests. See pkg/cdssim.
//
// Environment:
//
//	CDS_SIM_ADDR     listen address (default :8766)
//	CDS_SIM_API_KEY  if set, require this X-API-Key, like the OpenRVDAS API
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/OceanDataTools/grafana-openrvdas-datasource/pkg/cdssim"
)

func main() {
	addr := os.Getenv("CDS_SIM_ADDR")
	if addr == "" {
		addr = ":8766"
	}
	logger := log.New(os.Stderr, "cds-sim: ", log.LstdFlags)

	sim := cdssim.New(cdssim.ShipFields())
	sim.APIKey = os.Getenv("CDS_SIM_API_KEY")
	sim.Logger = logger

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go sim.Run(ctx)

	srv := &http.Server{Addr: addr, Handler: sim, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	auth := "no API key (direct mode)"
	if sim.APIKey != "" {
		auth = "API key required (proxy mode)"
	}
	logger.Printf("listening on %s, %s", addr, auth)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}
