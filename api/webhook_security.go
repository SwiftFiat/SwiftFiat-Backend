package api

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// CryptomusWebhookValidator handles all webhook security validations
type CryptomusWebhookValidator struct {
	// IP whitelist for Cryptomus
	allowedIPs map[string]bool
	// IP ranges for Cryptomus (CIDR)
	allowedRanges []*net.IPNet
	// Rate limiter for webhook requests
	rateLimiter *rate.Limiter
	// Request ID tracking
	requestIDLock sync.RWMutex
	processedIDs  map[string]time.Time
	// Max age for webhook timestamps (5 minutes)
	maxWebhookAge time.Duration
}

// NewCryptomusWebhookValidator creates a new webhook validator
// Cryptomus publishes their IP ranges - these should be updated periodically
// Reference: https://cryptomus.com/docs/merchant
func NewCryptomusWebhookValidator() *CryptomusWebhookValidator {
	validator := &CryptomusWebhookValidator{
		allowedIPs:    make(map[string]bool),
		rateLimiter:   rate.NewLimiter(rate.Limit(100), 10), // 100 req/sec with burst of 10
		processedIDs:  make(map[string]time.Time),
		maxWebhookAge: 5 * time.Minute,
	}

	// Add known Cryptomus IP ranges (these should be fetched from config or their docs)
	// Reference: https://cryptomus.com/docs/merchant
	cryptomusRanges := []string{
		"91.227.144.54/32", // Cryptomus verified IP
	}

	for _, cidr := range cryptomusRanges {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
			validator.allowedRanges = append(validator.allowedRanges, ipNet)
		}
	}

	// Also allow direct IPs for local testing
	validator.allowedIPs["127.0.0.1"] = true
	validator.allowedIPs["::1"] = true

	return validator
}

// ValidateSourceIP checks if the request came from an allowed Cryptomus IP
func (v *CryptomusWebhookValidator) ValidateSourceIP(clientIP string) error {
	// Check direct IP match
	if allowed, exists := v.allowedIPs[clientIP]; exists && allowed {
		return nil
	}

	// Parse the IP
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", clientIP)
	}

	// Check against ranges
	for _, ipRange := range v.allowedRanges {
		if ipRange.Contains(ip) {
			return nil
		}
	}

	return fmt.Errorf("webhook request from unauthorized IP: %s", clientIP)
}

// ValidateWebhookTimestamp checks if the webhook is not too old
// This prevents replay attacks of old webhooks
func (v *CryptomusWebhookValidator) ValidateWebhookTimestamp(timestamp int64) error {
	webhookTime := time.Unix(timestamp, 0)
	age := time.Since(webhookTime)

	if age < 0 {
		return fmt.Errorf("webhook timestamp is in the future (clock skew: %v)", age)
	}

	if age > v.maxWebhookAge {
		return fmt.Errorf("webhook is too old (age: %v, max: %v)", age, v.maxWebhookAge)
	}

	return nil
}

// CheckRateLimit uses token bucket algorithm to rate limit webhook requests
func (v *CryptomusWebhookValidator) CheckRateLimit(ctx context.Context) error {
	if !v.rateLimiter.Allow() {
		return fmt.Errorf("rate limit exceeded for webhook requests")
	}
	return nil
}

// TrackWebhookRequest ensures the same webhook (by request ID) isn't processed twice
// Prevents duplicate processing from retries
func (v *CryptomusWebhookValidator) TrackWebhookRequest(requestID string, timestamp time.Time) error {
	v.requestIDLock.Lock()
	defer v.requestIDLock.Unlock()

	// Clean up old entries (older than max webhook age)
	cutoff := time.Now().Add(-v.maxWebhookAge)
	for id, ts := range v.processedIDs {
		if ts.Before(cutoff) {
			delete(v.processedIDs, id)
		}
	}

	// Check if we've already processed this request
	if _, exists := v.processedIDs[requestID]; exists {
		return fmt.Errorf("webhook request already processed: %s", requestID)
	}

	// Record this request
	v.processedIDs[requestID] = timestamp
	return nil
}

// GetClientIP extracts the real client IP from request headers
// Handles X-Forwarded-For, X-Real-IP, and direct RemoteAddr
func GetClientIP(remoteAddr string, forwardedFor string, realIP string) string {
	// Check X-Real-IP first (most reliable for proxies)
	if realIP != "" {
		return realIP
	}

	// Check X-Forwarded-For (might contain multiple IPs, use first)
	if forwardedFor != "" {
		if ip, _, err := net.SplitHostPort(forwardedFor); err == nil {
			return ip
		}
		return forwardedFor
	}

	// Fall back to remote address
	if ip, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return ip
	}

	return remoteAddr
}
