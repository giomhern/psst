// Package server owns the HTTP server's startup and shutdown lifecycle.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/giomhern/psst/internal/sessions/httpapi"
)

// Run serves HTTP until ctx is canceled or serving fails. It waits for shutdown
// before returning. The caller owns ctx and logger; Run owns the listener.
func Run(ctx context.Context, addr string, logger *slog.Logger) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return serve(ctx, listener, logger)
}

func serve(ctx context.Context, listener net.Listener, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           httpapi.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	// One result fits even if cancellation wins the select. The receiver below
	// always joins this goroutine before returning.
	result := make(chan error, 1)
	go func() { result <- srv.Serve(listener) }()
	logger.Info("http_listening", "addr", listener.Addr().String())

	select {
	case err := <-result:
		return servingError(err)
	case <-ctx.Done():
		// The request lifetime has ended; shutdown needs its own bounded context.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, srv.Close())
		}
		serveErr := servingError(<-result)
		if err := errors.Join(shutdownErr, serveErr); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

func servingError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve HTTP: %w", err)
}
