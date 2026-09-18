package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modigo/runner/auth"
	"github.com/modigo/runner/config"
	"github.com/modigo/runner/executor"
	"github.com/modigo/runner/handler"
	"github.com/modigo/runner/ratelimit"
)

func main() {
	cfg := config.Load()

	// Fail fast — never run with no auth secret in production
	if !cfg.HasAuth() {
		log.Fatal("FATAL: AUTH_SECRET is not set. Set it in your .env or environment. Refusing to start without auth.")
	}

	log.Printf("=== Modigo Runner ===")
	log.Printf("Port:          %d", cfg.Port)
	log.Printf("Docker Socket: %s", cfg.DockerSocket)
	log.Printf("Image Prefix:  %s", cfg.ImagePrefix)
	log.Printf("Timeout:       %s", cfg.ExecTimeout)
	log.Printf("Memory Limit:  %s", cfg.MaxMemory)
	log.Printf("CPU Quota:     %d/%d", cfg.CPUQuota, cfg.CPUPeriod)
	log.Printf("Pool Size:     %d", cfg.PoolSize)
	log.Printf("Max Concurrent:%d containers", cfg.MaxConcurrent)
	log.Printf("Auth:          %v (JWT validation with Laravel)", cfg.HasAuth())

	// Initialize Docker client
	limits := executor.DefaultLimits().
		WithMemory(cfg.MaxMemory).
		WithCPUQuota(cfg.CPUQuota).
		WithPidsLimit(cfg.MaxPID).
		WithTimeout(int(cfg.ExecTimeout.Seconds()))

	docker, err := executor.NewDockerClient(cfg.DockerSocket, cfg.ImagePrefix, limits)
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}

	// Clean up orphaned containers from previous crashes
	executor.CleanupOrphanedContainers(docker)

	// Background reaper: force-remove leaked containers every 5 min so a crash
	// or missed in-band timeout can never pin concurrency slots for good.
	executor.StartZombieReaper(docker)

	// Initialize lab manager for isolated student↔target networking
	labMgr, labErr := executor.NewLabManager(cfg.DockerSocket, cfg.ImagePrefix)
	if labErr != nil {
		log.Printf("[warn] Lab manager init failed (labs disabled): %v", labErr)
	}
	if labMgr != nil {
		labMgr.CleanupOrphanedLabs(context.Background())
	}

	// Concurrency limiter — max simultaneous Docker containers
	containerSem := executor.NewSemaphore(cfg.MaxConcurrent)

	// Initialize rate limiter
	limiter := ratelimit.NewLimiter(cfg.RateLimitRPS, cfg.RateLimitDaily) // Pre-warm container pool
	supportedLangs := []string{"python", "javascript", "c", "cpp", "go", "java", "rust", "php", "cyber"}
	pool := executor.NewPool(docker, cfg.PoolSize, supportedLangs)
	defer pool.Stop()

	// Wire pool reference for health checks
	handler.SetPool(pool)
	handler.SetDockerClient(docker)
	handler.SetSemaphore(containerSem)

	// Auth middleware — validates JWTs signed by Laravel
	authMw := auth.Middleware(cfg.AuthSecret)

	// Set up routes
	mux := http.NewServeMux()

	// Public endpoints (no auth)
	mux.HandleFunc("/health", handler.HealthCheck)

	// Protected API routes (JWT required)
	mux.Handle("/api/v1/run", authMw(ratelimitMiddleware(limiter, http.HandlerFunc(handler.Execute(docker, limiter, containerSem)))))
	mux.Handle("/api/v1/languages", authMw(http.HandlerFunc(handler.Languages)))

	// WebSocket (JWT required — browser passes token via query param)
	mux.Handle("/ws/run", authMw(http.HandlerFunc(handler.WebSocketHandler(docker, cfg, containerSem, labMgr))))

	// Stats (protected by STATS_KEY for monitoring)
	mux.HandleFunc("/stats", handler.Stats(containerSem, pool))

	// Wrap with CORS + logging
	wrappedMux := corsMiddleware(cfg)(logMiddleware(mux))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      wrappedMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Server listening on :%d", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	sig := <-done
	log.Printf("Received signal %v, shutting down...", sig)

	// Signal WebSocket handlers to reject new runs
	handler.TriggerShutdown(docker)

	// Wait briefly for in-flight requests to complete
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	// Give WebSocket handlers time to clean up their containers
	time.Sleep(2 * time.Second)

	log.Printf("Server stopped (active sessions remaining: %d)", handler.GetActiveSessions())
}

func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := false
			for _, o := range cfg.AllowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}
			if allowed {
				if origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		if !strings.HasPrefix(r.URL.Path, "/health") {
			log.Printf("[http] %s %s %s %dms", r.Method, r.URL.Path, r.RemoteAddr, duration.Milliseconds())
		}
	})
}

func ratelimitMiddleware(l *ratelimit.Limiter, next http.Handler) http.Handler {
	return l.Middleware(next)
}
