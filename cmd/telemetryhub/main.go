// Command telemetryhub runs a simulated press line and serves a live OEE dashboard.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/hub"
	"github.com/wasuwe/machine-telemetry-hub/internal/simulator"
	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
	"github.com/wasuwe/machine-telemetry-hub/internal/web"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	count := flag.Int("machines", 12, "number of simulated machines")
	seed := flag.Uint64("seed", uint64(time.Now().UnixNano()), "random seed (same seed = same behaviour)")
	flag.Parse()

	if err := run(*addr, *count, *seed); err != nil {
		slog.Error("telemetryhub stopped", "err", err)
		os.Exit(1)
	}
}

func run(addr string, count int, seed uint64) error {
	// Ctrl+C / SIGTERM cancels ctx, which stops every goroutine below.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	machines := simulator.DefaultMachines(count)
	h := hub.New(machines, time.Now)
	events := make(chan telemetry.Event, 256)

	go simulator.New(machines, seed).Run(ctx, events)
	go h.Run(ctx, events, time.Second)

	srv := &http.Server{
		Addr:              addr,
		Handler:           web.NewHandler(h),
		ReadHeaderTimeout: 5 * time.Second,
		// Request contexts derive from ctx, so open SSE streams end on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("dashboard ready", "url", "http://localhost"+displayPort(addr), "machines", count)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func displayPort(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return ":" + port
	}
	return addr
}
