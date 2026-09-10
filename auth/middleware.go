package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const (
	UserIDKey    contextKey = "user_id"
	CustomerIDKey contextKey = "customer_id"
	PlanKey      contextKey = "plan"
	AuthMethodKey contextKey = "auth_method"
)

// Claims represents the JWT payload issued by Laravel.
type Claims struct {
	UserID     string `json:"user_id"`
	Sub        string `json:"sub"`
	CustomerID string `json:"customer_id,omitempty"`
	Plan       string `json:"plan,omitempty"`
	Exp        int64  `json:"exp"`
	Iat        int64  `json:"iat,omitempty"`
}

// Middleware validates requests using HMAC-SHA256 JWTs.
// The shared secret must match AUTH_SECRET on the Laravel side.
func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Dev mode: no secret configured = allow all
			if secret == "" {
				ctx := context.WithValue(r.Context(), AuthMethodKey, "none")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			claims, err := extractClaims(r, secret)
			if err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, AuthMethodKey, "jwt")
			if claims.CustomerID != "" {
				ctx = context.WithValue(ctx, CustomerIDKey, claims.CustomerID)
			}
			if claims.Plan != "" {
				ctx = context.WithValue(ctx, PlanKey, claims.Plan)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserID extracts the user ID from the request context.
func GetUserID(r *http.Request) string {
	if v, ok := r.Context().Value(UserIDKey).(string); ok {
		return v
	}
	return ""
}

// GetCustomerID extracts the customer ID from the request context.
func GetCustomerID(r *http.Request) string {
	if v, ok := r.Context().Value(CustomerIDKey).(string); ok {
		return v
	}
	return ""
}

// GetPlan extracts the plan from the request context.
func GetPlan(r *http.Request) string {
	if v, ok := r.Context().Value(PlanKey).(string); ok {
		return v
	}
	return ""
}

// extractClaims tries to get a JWT from:
// 1. Authorization: Bearer <token> header (REST API)
// 2. Sec-WebSocket-Protocol header (browser WebSocket — token never in URL)
// 3. ?token=<token> query param (fallback for non-browser clients, testing)
func extractClaims(r *http.Request, secret string) (*Claims, error) {
	token := ""

	// Authorization header (standard for REST)
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Sec-WebSocket-Protocol header (browser WebSocket auth — clean, no URL leakage)
	if token == "" {
		if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
			// The protocol field may contain multiple comma-separated values;
			// the JWT is the first one
			token = strings.TrimSpace(strings.SplitN(proto, ",", 2)[0])
		}
	}

	// Query param fallback (non-browser clients, testing, backward compat)
	if token == "" {
		token = r.URL.Query().Get("token")
	}

	if token == "" {
		return nil, fmt.Errorf("authentication required")
	}

	return validateJWT(token, secret)
}

// validateJWT validates a standard HMAC-SHA256 JWT.
func validateJWT(token, secret string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Verify header
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid token header")
	}
	var header map[string]interface{}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("invalid token header")
	}
	if header["alg"] != "HS256" {
		return nil, fmt.Errorf("unsupported algorithm: %v", header["alg"])
	}

	// Verify signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid token signature")
	}

	// Decode payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid token payload")
	}
	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("invalid token payload")
	}

	// Check expiry
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	// Normalize user_id
	if claims.UserID == "" {
		claims.UserID = claims.Sub
	}
	if claims.UserID == "" {
		return nil, fmt.Errorf("no user_id in token")
	}

	return &claims, nil
}
