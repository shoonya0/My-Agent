package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const defaultShutdownTimeout = 10 * time.Second

// Start binds the given handler to addr (e.g. ":8080"), serves HTTP traffic,
// and blocks until SIGINT or SIGTERM is received. It then gracefully drains
// in-flight requests within the shutdown timeout before returning.
func Start(addr string, handler http.Handler) error {
	// srv is the HTTP server that will be used to serve the requests
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// errCh is a channel that will be used to send errors from the HTTP server
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Listening on http://localhost%s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// quit is a channel that will be used to receive signals from the operating system
	quit := make(chan os.Signal, 1)
	// signal.Notify is used to notify the channel when the signal is received
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Printf("Received %s, shutting down gracefully...", sig)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("forced shutdown: %w", err)
	}

	log.Println("Server stopped cleanly")
	return nil
}
