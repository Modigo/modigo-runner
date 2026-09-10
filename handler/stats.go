package handler

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/modigo/runner/executor"
)

// Stats returns service metrics, protected by STATS_KEY.
func Stats(sem *executor.Semaphore, pool *executor.Pool) http.HandlerFunc {
	statsKey := os.Getenv("STATS_KEY")

	return func(w http.ResponseWriter, r *http.Request) {
		// Auth check: if STATS_KEY is set, require it
		if statsKey != "" {
			provided := r.Header.Get("X-Stats-Key")
			if provided == "" {
				provided = r.URL.Query().Get("key")
			}
			if provided != statsKey {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active_sessions":      GetActiveSessions(),
			"containers_running":   sem.Current(),
			"containers_available": sem.Available(),
			"containers_queue":     sem.QueueLen(),
			"pool":                 pool.Stats(),
			"service":              "modigo-runner",
		})
	}
}
