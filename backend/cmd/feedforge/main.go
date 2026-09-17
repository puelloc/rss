package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"feedforge/internal/api"
	"feedforge/internal/poller"
	"feedforge/internal/store"
)

func main() {
	dataDir := env("DATA_DIR", "./data")
	webDir := env("WEB_DIR", "./web/dist")
	addr := env("LISTEN_ADDR", ":8080")

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	st, err := store.Open(filepath.Join(dataDir, "feedforge.db"))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	p := poller.New(st)
	a := api.New(st, p)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}))

	a.Routes(r)
	if webDir != "" {
		// SPA fallback: unknown paths resolve to a file in webDir or index.html.
		spa := api.StaticHandler(webDir)
		r.NotFound(spa.ServeHTTP)
	}

	go warmup(p, st)
	go scheduler(p, st)

	log.Printf("FeedForge listening on %s (web: %s, data: %s)", addr, webDir, dataDir)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// warmup pre-populates the cache so the first request is fast.
func warmup(p *poller.Poller, st *store.Store) {
	time.Sleep(2 * time.Second)
	feeds, err := st.List()
	if err != nil {
		log.Printf("warmup list: %v", err)
		return
	}
	for _, f := range feeds {
		if !f.Enabled {
			continue
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if _, _, err := p.Get(ctx, f, "", false); err != nil {
				log.Printf("warmup %s (%s): %v", f.Name, f.ID, err)
			}
		}()
	}
}

// scheduler refreshes enabled feeds on their configured interval.
func scheduler(p *poller.Poller, st *store.Store) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		feeds, err := st.List()
		if err != nil {
			log.Printf("scheduler list: %v", err)
			continue
		}
		for _, f := range feeds {
			if !f.Enabled {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			if _, _, err := p.Get(ctx, f, "", false); err != nil {
				log.Printf("refresh %s (%s): %v", f.Name, f.ID, err)
			}
			cancel()
		}
	}
}

