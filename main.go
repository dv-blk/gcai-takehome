package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"contractanalyzer/agent"
	"contractanalyzer/analyzer"
	"contractanalyzer/database"
	"contractanalyzer/handlers"
	"contractanalyzer/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Initialize storage directories
	if err := storage.Init(); err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Open database
	db, err := database.Open("data/contractanalyzer.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Start or connect to the OpenCode bridge
	var bridgeServer *agent.BridgeServer
	bridgeURL := os.Getenv("OPENCODE_BRIDGE_URL")

	if bridgeURL != "" {
		// Use external bridge (for development/debugging)
		log.Printf("Using external bridge at %s", bridgeURL)
	} else {
		// Spawn the bridge as a subprocess
		log.Println("Starting OpenCode bridge...")
		ctx := context.Background()
		bridgeServer, err = agent.StartBridge(ctx, func(format string, args ...interface{}) {
			log.Printf(format, args...)
		})
		if err != nil {
			log.Fatalf("Failed to start bridge: %v", err)
		}
		bridgeURL = bridgeServer.BaseURL()
		log.Printf("Bridge started at %s", bridgeURL)
	}

	// Initialize analysis queue with the bridge URL
	analyzer.InitQueue(db, bridgeURL)

	// Parse all templates
	tmpl, err := parseTemplates()
	if err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	}

	// Create handler with database
	h := handlers.New(tmpl, db)

	// Create router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Static files
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Routes
	r.Get("/", h.Index)
	r.Get("/new", h.NewSubmissionForm)
	r.Post("/submissions", h.CreateSubmission)

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	server := &http.Server{Addr: "0.0.0.0:8080", Handler: r}
	go func() {
		log.Println("Server starting on http://0.0.0.0:8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigCh
	log.Println("Shutting down...")

	// Stop the bridge if we started it
	if bridgeServer != nil {
		log.Println("Stopping bridge...")
		bridgeServer.Stop()
	}

	// Shutdown HTTP server
	server.Shutdown(context.Background())
	log.Println("Shutdown complete")
}

func parseTemplates() (*template.Template, error) {
	// Parse layout first
	tmpl, err := template.ParseFiles("templates/layout.html")
	if err != nil {
		return nil, err
	}

	// Parse all other templates
	patterns := []string{
		"templates/*.html",
		"templates/partials/*.html",
	}

	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file == "templates/layout.html" {
				continue // Already parsed
			}
			_, err = tmpl.ParseFiles(file)
			if err != nil {
				return nil, err
			}
		}
	}

	return tmpl, nil
}
