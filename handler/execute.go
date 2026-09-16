package handler

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/modigo/runner/auth"
	"github.com/modigo/runner/executor"
	"github.com/modigo/runner/protocol"
	"github.com/modigo/runner/ratelimit"
)

// Execute handles POST /api/v1/run for non-interactive code execution.
// Auth is handled by middleware — user_id is in context.
func Execute(docker *executor.DockerClient, limiter *ratelimit.Limiter, sem *executor.Semaphore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		// Read body
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
		if err != nil {
			http.Error(w, `{"error":"Failed to read request body"}`, http.StatusBadRequest)
			return
		}

		var req protocol.RESTRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, `{"error":"Invalid JSON"}`, http.StatusBadRequest)
			return
		}

		// Validate
		if req.Language == "" {
			http.Error(w, `{"error":"language is required"}`, http.StatusBadRequest)
			return
		}
		if len(req.Files) == 0 && req.Code == "" {
			http.Error(w, `{"error":"Either files or code is required"}`, http.StatusBadRequest)
			return
		}

		// Rate limit by user_id (from JWT) or IP fallback
		userID := auth.GetUserID(r)
		rateKey := userID
		if rateKey == "" {
			rateKey = r.RemoteAddr
		}
		if limiter != nil && !limiter.Allow(rateKey) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":"Rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		// Acquire concurrency slot — bounded wait. Cloudflare 524s any HTTP
		// request without a response after ~100s; queueing past that point
		// just turns overload into cryptic edge errors. Fail fast with 503
		// (which passes through Cloudflare untouched) and let clients retry.
		acquireCtx, cancelAcquire := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancelAcquire()
		if !sem.AcquireContext(acquireCtx) {
			w.Header().Set("Retry-After", "5")
			http.Error(w, `{"error":"Service at capacity — try again shortly"}`, http.StatusServiceUnavailable)
			return
		}
		defer sem.Release()

		plan := auth.GetPlan(r)
		log.Printf("[api] execute: user=%s plan=%s lang=%s files=%d", userID, plan, req.Language, len(req.Files))

		// Execute
		resp, err := docker.RunNonInteractive(r.Context(), req)
		if err != nil {
			log.Printf("[api] execute error: %v", err)
			http.Error(w, `{"error":"Execution failed: `+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
