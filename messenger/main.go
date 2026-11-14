package main

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

	"messenger/agent"
	"messenger/bluebubbles"
	"messenger/check"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load("../.env")
	if err := run(getenv); err != nil {
		log.Fatal(err)
	}
}

func run(getenv func(string, string) string) error {
	var (
		addr       = "127.0.0.1:" + getenv("PORT", "8080")
		bbURL      = getenv("BLUEBUBBLES_URL", "http://127.0.0.1:1234")
		bbPassword = getenv("BLUEBUBBLES_PASSWORD", "")
		checkURL   = getenv("DEALCHECK_URL", "")
		geminiKey  = getenv("GEMINI_API_KEY", "")
		model      = getenv("GEMINI_MODEL", "gemini-4-hydrogen")
		thinking   = getenv("GEMINI_THINKING", "low")
		fallback   = getenv("GEMINI_FALLBACK_MODEL", "gemini-3.8-flash")
		nvidiaKey  = getenv("NVIDIA_API_KEY", "")
		vlModel    = getenv("NEMOTRON_MODEL", "nvidia/nemotron-3.5-super-vl-120b-a12b")
	)

	if bbPassword == "" {
		return errors.New("BLUEBUBBLES_PASSWORD is not set")
	}
	if geminiKey == "" {
		return errors.New("GEMINI_API_KEY is not set")
	}
	if nvidiaKey == "" {
		return errors.New("NVIDIA_API_KEY is not set")
	}

	var checker bluebubbles.Checker = check.Fake{}
	if checkURL != "" {
		checker = check.NewClient(checkURL)
	} else {
		log.Println("DEALCHECK_URL not set, using fake checks")
	}

	svc := bluebubbles.NewService(bluebubbles.NewClient(bbURL, bbPassword), checker, agent.New(agent.NewGemini(geminiKey, model, thinking), agent.NewGemini(geminiKey, fallback, thinking), agent.NewVision(nvidiaKey, vlModel)))
	bb := bluebubbles.NewHandler(svc)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("POST /webhook", bb.Webhook)

	s := &http.Server{
		Addr:              addr,
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

	go svc.Poll(ctx)

	go func() {
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errs <- fmt.Errorf("server shut down: %v", err)
		}
	}()

	log.Printf("Server listening on %s", s.Addr)

	select {
	case <-ctx.Done():
		log.Println("Shutting down server...")
	case err := <-errs:
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shut down: %v", err)
	}

	return nil
}

func getenv(k, dv string) string {
	v := os.Getenv(k)

	if v == "" {
		return dv
	}

	return v
}
