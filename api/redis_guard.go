package api

// redis_guard.go — Startup Redis security validation for SwiftFiat.
//
// Redis is SwiftFiat's auth source-of-truth: session tokens, OTP codes, and
// brute-force counters all live there. If Redis is misconfigured (wrong
// maxmemory-policy, no persistence), the auth system degrades silently.
//
// This file provides:
//   1. ValidateRedisSecurityConfig()  — called at startup, fatal on violations
//   2. RedisHealthMiddleware()        — per-request Redis availability check
//   3. Auth-safe degraded-mode logic  — rejects auth requests when Redis is down

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/gin-gonic/gin"
)

// ── Startup validation ────────────────────────────────────────────────────────

// RedisSecurityReport is returned by ValidateRedisSecurityConfig.
type RedisSecurityReport struct {
	MaxMemoryPolicy string
	Persistence     string
	Warnings        []string
	Errors          []string
	Passed          bool
}

// ValidateRedisSecurityConfig checks critical Redis configuration at startup.
// Logs warnings for degraded-but-survivable config, returns an error for
// configurations that make the auth system unsafe to operate.
//
// Call this in NewServer() before registering routes.
func ValidateRedisSecurityConfig(ctx context.Context, r *redis.RedisService, l *logging.Logger) error {
	report := &RedisSecurityReport{Passed: true}

	// ── 1. maxmemory-policy ───────────────────────────────────────────────
	// Any LRU/LFU/random eviction policy means Redis can silently delete
	// active session tokens, OTP codes, or brute-force lock keys.
	// noeviction is the only safe setting for auth data.
	policy, err := r.ConfigGet(ctx, "maxmemory-policy")
	if err != nil {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("could not read maxmemory-policy: %v (ensure Redis ACLs allow CONFIG GET)", err))
	} else {
		report.MaxMemoryPolicy = policy
		switch policy {
		case "noeviction", "":
			// "" means no limit set — safe because eviction never triggers
		default:
			report.Errors = append(report.Errors,
				fmt.Sprintf("UNSAFE maxmemory-policy=%q — auth keys may be silently evicted. "+
					"Set 'maxmemory-policy noeviction' in redis.conf", policy))
			report.Passed = false
		}
	}

	// ── 2. Persistence mode ───────────────────────────────────────────────
	// If both RDB and AOF are disabled, a Redis restart wipes all sessions.
	// Users are silently logged out; attackers with pre-rotation refresh tokens
	// can potentially re-use them if the family tracking is also lost.
	aofEnabled, _ := r.ConfigGet(ctx, "appendonly")
	saveConfig, _ := r.ConfigGet(ctx, "save")
	report.Persistence = fmt.Sprintf("AOF=%s RDB=%q", aofEnabled, saveConfig)

	if aofEnabled != "yes" && (saveConfig == "" || saveConfig == `""`) {
		report.Warnings = append(report.Warnings,
			"Redis has NO persistence configured (no AOF, no RDB snapshots). "+
				"A Redis restart will log out all users and clear brute-force counters. "+
				"Enable AOF with 'appendonly yes' in redis.conf.")
	}

	// ── 3. Password / AUTH ────────────────────────────────────────────────
	requirePass, _ := r.ConfigGet(ctx, "requirepass")
	if requirePass == "" {
		report.Warnings = append(report.Warnings,
			"Redis has no password set (requirepass is empty). "+
				"Anyone with network access to the Redis port can read all session tokens.")
	}

	// ── Emit results ──────────────────────────────────────────────────────
	for _, w := range report.Warnings {
		l.Warn(fmt.Sprintf("[REDIS SECURITY WARNING] %s", w))
	}
	for _, e := range report.Errors {
		l.Error(fmt.Sprintf("[REDIS SECURITY ERROR] %s", e))
	}

	if !report.Passed {
		return fmt.Errorf("redis security validation failed: %s",
			strings.Join(report.Errors, "; "))
	}

	l.Info(fmt.Sprintf("[REDIS SECURITY] config OK — policy=%s persistence=%s",
		report.MaxMemoryPolicy, report.Persistence))
	return nil
}

// ── Per-request health check ──────────────────────────────────────────────────

// RedisHealthMiddleware rejects auth-sensitive requests when Redis is down.
// Without Redis, session validation is impossible — we fail closed rather
// than allowing requests through on a broken JWT-only path.
//
// Applied selectively: auth endpoints and authenticated routes only.
// Static assets and health checks bypass it.
func RedisHealthMiddleware(r *redis.RedisService) gin.HandlerFunc {
	var (
		lastCheck  time.Time
		lastResult bool
	)
	checkInterval := 5 * time.Second

	return func(ctx *gin.Context) {
		now := time.Now()

		// Rate-limit the health check to avoid slamming Redis on every request
		if now.Sub(lastCheck) > checkInterval {
			pingCtx, cancel := context.WithTimeout(ctx.Request.Context(), 500*time.Millisecond)
			err := r.Ping(pingCtx)
			cancel()

			lastResult = err == nil
			lastCheck = now

			if !lastResult {
				ctx.AbortWithStatusJSON(http.StatusServiceUnavailable,
					basemodels.NewError("authentication service temporarily unavailable — please try again shortly"))
				return
			}
		}

		if !lastResult {
			ctx.AbortWithStatusJSON(http.StatusServiceUnavailable,
				basemodels.NewError("authentication service temporarily unavailable — please try again shortly"))
			return
		}

		ctx.Next()
	}
}
