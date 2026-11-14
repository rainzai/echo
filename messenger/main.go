package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		txt, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "REQUEST_SIZE", http.StatusRequestEntityTooLarge)
				return
			}

			http.Error(w, "INTERNAL_ERROR", http.StatusInternalServerError)
			return
		}

		log.Print(string(txt))
	})

	s := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errs := make(chan error, 1)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errs <- fmt.Errorf("server shut down: %v", err)
		}
	}()

	log.Printf("Server listening on %s", s.Addr)

	select {
	case <-ctx.Done():
		log.Println("Shutting down server...")
		stop()
	case err := <-errs:
		log.Fatal(err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced to shut down: %v", err)
	}

	log.Print("Server stopped")
}
