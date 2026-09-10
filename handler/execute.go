package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

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

		// Acquire concurrency slot — blocks if at capacity
		sem.Acquire()
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
