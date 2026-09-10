package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateJWT_ValidToken(t *testing.T) {
	secret := "test-secret"
	token := generateTestJWT(secret, map[string]interface{}{
		"user_id": "123",
		"plan":    "pro",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	claims, err := validateJWT(token, secret)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if claims.UserID != "123" {
		t.Fatalf("expected user_id 123, got %s", claims.UserID)
	}
	if claims.Plan != "pro" {
		t.Fatalf("expected plan pro, got %s", claims.Plan)
	}
}

func TestValidateJWT_ExpiredToken(t *testing.T) {
	secret := "test-secret"
	token := generateTestJWT(secret, map[string]interface{}{
		"user_id": "123",
		"exp":     1000000000, // 2001
	})

	_, err := validateJWT(token, secret)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateJWT_WrongSecret(t *testing.T) {
	token := generateTestJWT("wrong-secret", map[string]interface{}{
		"user_id": "123",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	_, err := validateJWT(token, "correct-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestValidateJWT_InvalidFormat(t *testing.T) {
	_, err := validateJWT("not-a-jwt", "secret")
	if err == nil {
		t.Fatal("expected error for invalid format, got nil")
	}
}

func TestExtractClaims_FromHeader(t *testing.T) {
	secret := "test-secret"
	token := generateTestJWT(secret, map[string]interface{}{
		"user_id": "456",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	claims, err := extractClaims(req, secret)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if claims.UserID != "456" {
		t.Fatalf("expected user_id 456, got %s", claims.UserID)
	}
}

func TestExtractClaims_FromQuery(t *testing.T) {
	secret := "test-secret"
	token := generateTestJWT(secret, map[string]interface{}{
		"user_id": "789",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/?token="+token, nil)

	claims, err := extractClaims(req, secret)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if claims.UserID != "789" {
		t.Fatalf("expected user_id 789, got %s", claims.UserID)
	}
}

func TestExtractClaims_FromWebSocketProtocol(t *testing.T) {
	secret := "test-secret"
	token := generateTestJWT(secret, map[string]interface{}{
		"user_id": "999",
		"plan":    "pro",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/run", nil)
	req.Header.Set("Sec-WebSocket-Protocol", token)

	claims, err := extractClaims(req, secret)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if claims.UserID != "999" {
		t.Fatalf("expected user_id 999, got %s", claims.UserID)
	}
}

func TestExtractClaims_PriorityOrder(t *testing.T) {
	// Authorization header should take priority over Sec-WebSocket-Protocol
	secret := "test-secret"
	headerToken := generateTestJWT(secret, map[string]interface{}{
		"user_id": "from-header",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	protoToken := generateTestJWT(secret, map[string]interface{}{
		"user_id": "from-protocol",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+headerToken)
	req.Header.Set("Sec-WebSocket-Protocol", protoToken)

	claims, err := extractClaims(req, secret)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if claims.UserID != "from-header" {
		t.Fatalf("expected user_id from-header, got %s", claims.UserID)
	}
}

func TestGenerateAndValidateJWT_RoundTrip(t *testing.T) {
	secret := "round-trip-test"
	token := generateTestJWT(secret, map[string]interface{}{
		"user_id": "42",
		"plan":    "free",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})

	claims, err := validateJWT(token, secret)
	if err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	if claims.UserID != "42" {
		t.Fatalf("expected user_id 42, got %s", claims.UserID)
	}
	if claims.Plan != "free" {
		t.Fatalf("expected plan free, got %s", claims.Plan)
	}
}

// generateTestJWT creates a JWT matching PHP's base64UrlEncode output.
func generateTestJWT(secret string, payload map[string]interface{}) string {
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payloadJSON, _ := json.Marshal(payload)

	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	body := base64.RawURLEncoding.EncodeToString(payloadJSON)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(header + "." + body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return header + "." + body + "." + sig
}
