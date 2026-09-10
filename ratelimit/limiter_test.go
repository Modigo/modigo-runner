package ratelimit

import (
	"testing"
)

func TestLimiter_Allow(t *testing.T) {
	l := NewLimiter(5, 100) // 5 RPS, 100 daily

	// Should allow first 5 requests
	for i := 0; i < 5; i++ {
		if !l.Allow("user1") {
			t.Fatalf("expected Allow to return true for request %d", i+1)
		}
	}

	// 6th request should be rate limited
	if l.Allow("user1") {
		t.Fatal("expected Allow to return false after exceeding RPS")
	}
}

func TestLimiter_DifferentKeys(t *testing.T) {
	l := NewLimiter(2, 100) // 2 RPS

	// User 1 uses their 2 requests
	l.Allow("user1")
	l.Allow("user1")

	// User 1 is limited
	if l.Allow("user1") {
		t.Fatal("user1 should be rate limited")
	}

	// User 2 is not affected
	if !l.Allow("user2") {
		t.Fatal("user2 should not be rate limited")
	}
}

func TestLimiter_DailyLimit(t *testing.T) {
	l := NewLimiter(1000, 3) // High RPS, low daily

	for i := 0; i < 3; i++ {
		if !l.Allow("user1") {
			t.Fatalf("expected Allow to return true for request %d", i+1)
		}
	}

	// 4th request should be rate limited (daily cap)
	if l.Allow("user1") {
		t.Fatal("expected Allow to return false after exceeding daily limit")
	}
}

func TestLimiter_RemainingDaily(t *testing.T) {
	l := NewLimiter(1000, 10)

	remaining := l.RemainingDaily("user1")
	if remaining != 10 {
		t.Fatalf("expected 10 remaining, got %d", remaining)
	}

	l.Allow("user1")
	l.Allow("user1")

	remaining = l.RemainingDaily("user1")
	if remaining != 8 {
		t.Fatalf("expected 8 remaining, got %d", remaining)
	}
}
