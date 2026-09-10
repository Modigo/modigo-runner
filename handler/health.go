package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/modigo/runner/executor"
)

// poolRef holds a reference to the pool for health checks.
var poolRef *executor.Pool

// dockerRef holds a reference to the Docker client for health checks.
var dockerRef *executor.DockerClient

// SetPool sets the pool reference for health checks.
func SetPool(p *executor.Pool) {
	poolRef = p
}

// SetDockerClient sets the Docker client reference for health checks.
func SetDockerClient(d *executor.DockerClient) {
	dockerRef = d
}

// HealthCheck returns service health status including pool readiness and Docker status.
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	httpStatus := http.StatusOK
	dockerOK := false

	// Ping Docker daemon
	if dockerRef != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := dockerRef.Ping(ctx); err != nil {
			status = "degraded"
			httpStatus = http.StatusServiceUnavailable
		} else {
			dockerOK = true
		}
	}

	if poolRef != nil && !poolRef.IsReady() {
		if status == "ok" {
			status = "warming_up"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      status,
		"service":     "modigo-runner",
		"pool_ready":  poolRef != nil && poolRef.IsReady(),
		"docker":      dockerOK,
		"active_sessions": GetActiveSessions(),
	})
}
