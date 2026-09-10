package ratelimit

import (
	"net/http"
	"sync"
	"time"

	"github.com/modigo/runner/auth"
)

// Limiter provides per-key rate limiting.
type Limiter struct {
	mu    sync.Mutex
	keys  map[string]*keyState
	rps   int
	daily int
}

type keyState struct {
	// Sliding window for RPS
	windowStart time.Time
	windowCount int

	// Daily counter
	dailyDate  string // "2006-01-02"
	dailyCount int
}

// NewLimiter creates a rate limiter with the given RPS and daily limits.
func NewLimiter(rps, daily int) *Limiter {
	l := &Limiter{
		keys:   make(map[string]*keyState),
		rps:    rps,
		daily:  daily,
	}
	go l.cleanup()
	return l
}

// Allow checks if a request from the given key is allowed.
// Returns true if allowed, false if rate limited.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	today := now.Format("2006-01-02")

	state, ok := l.keys[key]
	if !ok {
		state = &keyState{
			windowStart: now,
			dailyDate:   today,
		}
		l.keys[key] = state
	}

	// Reset daily counter if it's a new day
	if state.dailyDate != today {
		state.dailyDate = today
		state.dailyCount = 0
	}

	// Check daily limit
	if l.daily > 0 && state.dailyCount >= l.daily {
		return false
	}

	// Check RPS sliding window (1-second window)
	if now.Sub(state.windowStart) >= time.Second {
		state.windowStart = now
		state.windowCount = 0
	}

	if l.rps > 0 && state.windowCount >= l.rps {
		return false
	}

	state.windowCount++
	state.dailyCount++
	return true
}

// RemainingDaily returns the remaining daily executions for a key.
func (l *Limiter) RemainingDaily(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	state, ok := l.keys[key]
	if !ok || state.dailyDate != today {
		return l.daily
	}
	remaining := l.daily - state.dailyCount
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Middleware returns an HTTP middleware that rate-limits by JWT user_id.
// Falls back to IP address if no JWT is present.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Rate limit by user_id from JWT, falling back to IP
		key := auth.GetUserID(r)
		if key == "" {
			key = r.RemoteAddr
		}

		if !l.Allow(key) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("X-RateLimit-Limit", itoa(l.daily))
			w.Header().Set("X-RateLimit-Remaining", itoa(l.RemainingDaily(key)))
			http.Error(w, `{"error":"Rate limit exceeded. Try again later."}`, http.StatusTooManyRequests)
			return
		}

		w.Header().Set("X-RateLimit-Limit", itoa(l.daily))
		w.Header().Set("X-RateLimit-Remaining", itoa(l.RemainingDaily(key)))
		next.ServeHTTP(w, r)
	})
}

// cleanup periodically removes stale entries.
func (l *Limiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		today := now.Format("2006-01-02")
		for k, state := range l.keys {
			// Remove entries older than 1 day
			if state.dailyDate != today && now.Sub(state.windowStart) > 24*time.Hour {
				delete(l.keys, k)
			}
		}
		l.mu.Unlock()
	}
}

func itoa(i int) string {
	if i < 0 {
		return "0"
	}
	s := make([]byte, 0, 10)
	s = append(s, byte('0'+i%10))
	i /= 10
	for i > 0 {
		s = append([]byte{byte('0' + i%10)}, s...)
		i /= 10
	}
	return string(s)
}
