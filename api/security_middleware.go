package api

// security_middleware.go
//
// Three composable Gin middleware layers:
//
//  1. IPRateLimitMiddleware  – sliding-window per-IP rate limiter backed by
//     Redis sorted sets. Emits X-RateLimit-* headers on every response.
//
//  2. BruteForceMiddleware   – pre-handler gate that rejects requests when
//     an account is inside its 30-minute lockout window.
//
//  3. SessionBlockMiddleware – validates that the session embedded in the JWT
//     hasn't been force-blocked by an admin or fraud rule.
//
// Redis key layout (rate limiting)
//
//	rl:{tier}:{ip}       ZSET – member = request UUID, score = unix-ns timestamp
//	failed_login:{email} STRING – failed attempt counter
//	account_lock:{email} STRING – lockout marker (TTL = 30 min)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	redishelper "github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ── Rate limit tier definitions ───────────────────────────────────────────────

const (
	// Brute force
	BruteForceMaxAttempts = 5
	BruteForceLockTTL     = 30 * time.Minute
	BruteForceWindowTTL   = 15 * time.Minute

	// Tier limits (requests per window)
	RLGlobalLimit     = 200
	RLGlobalWindow    = 1 * time.Minute
	RLAuthLimit       = 20
	RLAuthWindow      = 1 * time.Minute
	RLSensitiveLimit  = 5
	RLSensitiveWindow = 1 * time.Minute
)

type rateLimitTier struct {
	name   string
	limit  int
	window time.Duration
}

var (
	tierGlobal    = rateLimitTier{"global", RLGlobalLimit, RLGlobalWindow}
	tierAuth      = rateLimitTier{"auth", RLAuthLimit, RLAuthWindow}
	tierSensitive = rateLimitTier{"sensitive", RLSensitiveLimit, RLSensitiveWindow}
)

// ── 1. IP Rate Limit Middleware ───────────────────────────────────────────────
//
// Sliding-window algorithm backed by a Redis sorted set.
// All four operations execute in a single pipeline (one network RTT).

func IPRateLimitMiddleware(r *redishelper.RedisService, tier rateLimitTier) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ip := realIP(ctx)
		key := fmt.Sprintf("rl:%s:%s", tier.name, ip)
		now := time.Now()
		windowStart := now.Add(-tier.window)
		member := uuid.NewString()

		pipe := r.Pipeline()
		// 1. Record this request
		zaddCmd := redishelper.PipeZAdd(pipe, ctx, key,
			float64(now.UnixNano()), member)
		// 2. Evict requests older than the window
		redishelper.PipeZRemRangeByScore(pipe, ctx, key,
			"-inf", strconv.FormatInt(windowStart.UnixNano(), 10))
		// 3. Count remaining
		zcardCmd := redishelper.PipeZCard(pipe, ctx, key)
		// 4. Self-cleaning TTL
		pipe.Expire(ctx, key, tier.window+5*time.Second)
		pipe.Exec(ctx)

		_ = zaddCmd

		count := zcardCmd.Val()
		remaining := int64(tier.limit) - count
		if remaining < 0 {
			remaining = 0
		}

		reset := now.Add(tier.window).Unix()
		ctx.Header("X-RateLimit-Limit", strconv.Itoa(tier.limit))
		ctx.Header("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
		ctx.Header("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		ctx.Header("X-RateLimit-Window", tier.window.String())

		if count > int64(tier.limit) {
			ctx.Header("Retry-After", strconv.Itoa(int(tier.window.Seconds())))
			ctx.AbortWithStatusJSON(http.StatusTooManyRequests, basemodels.NewError(
				fmt.Sprintf("rate limit exceeded — retry after %s", tier.window)))
			return
		}

		ctx.Next()
	}
}

// Tier shortcuts for wiring in server.go / auth router.
func GlobalRateLimit(r *redishelper.RedisService) gin.HandlerFunc {
	return IPRateLimitMiddleware(r, tierGlobal)
}

func AuthRateLimit(r *redishelper.RedisService) gin.HandlerFunc {
	return IPRateLimitMiddleware(r, tierAuth)
}

func SensitiveRateLimit(r *redishelper.RedisService) gin.HandlerFunc {
	return IPRateLimitMiddleware(r, tierSensitive)
}

// ── 2. Brute Force Middleware ─────────────────────────────────────────────────
//
// Checks the account-lock key BEFORE the login handler runs, so we never hit
// the DB when an account is in its 30-minute lockout window.
// The body is read, peeked, then re-injected so the handler can read it again.

func BruteForceMiddleware(r *redishelper.RedisService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// Read + restore body
		rawBody, err := io.ReadAll(ctx.Request.Body)
		if err != nil {
			ctx.Next()
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

		var body struct {
			Email string `json:"email"`
		}
		if err := json.Unmarshal(rawBody, &body); err != nil || body.Email == "" {
			ctx.Next()
			return
		}

		// Restore body for the actual handler
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

		if locked, ttl := IsAccountLocked(ctx, r, body.Email); locked {
			secs := int(ttl.Seconds())
			if secs < 0 {
				secs = 0
			}
			ctx.Header("Retry-After", strconv.Itoa(secs))
			ctx.AbortWithStatusJSON(http.StatusTooManyRequests, basemodels.NewError(
				fmt.Sprintf("account locked — too many failed attempts. Try again in %d minutes",
					(secs+59)/60)))
			return
		}

		ctx.Next()
	}
}

// RecordFailedLogin increments the failed-attempt counter for email.
// Returns (locked=true, attempts) when the account crosses the threshold.
// Call from the login handler after every password mismatch.
func RecordFailedLogin(ctx *gin.Context, r *redishelper.RedisService, email string) (locked bool, totalAttempts int) {
	failKey := failedLoginKey(email)
	lockKey := accountLockKey(email)

	attempts, err := r.Incr(ctx, failKey)
	if err != nil {
		return false, 0
	}
	if attempts == 1 {
		r.Expire(ctx, failKey, BruteForceWindowTTL)
	}

	if attempts >= BruteForceMaxAttempts {
		pipe := r.Pipeline()
		pipe.Set(ctx, lockKey,
			fmt.Sprintf("locked_at:%d", time.Now().Unix()),
			BruteForceLockTTL)
		pipe.Del(ctx, failKey)
		pipe.Exec(ctx)
		return true, int(attempts)
	}

	return false, int(attempts)
}

// ClearFailedLogins resets the attempt counter and removes any lock for email.
// Call from the login handler after a successful authentication.
func ClearFailedLogins(ctx *gin.Context, r *redishelper.RedisService, email string) {
	pipe := r.Pipeline()
	pipe.Del(ctx, failedLoginKey(email))
	pipe.Del(ctx, accountLockKey(email))
	pipe.Exec(ctx)
}

// IsAccountLocked returns (true, remainingTTL) when the account lock key exists.
func IsAccountLocked(ctx *gin.Context, r *redishelper.RedisService, email string) (bool, time.Duration) {
	locked, err := r.Get(ctx, accountLockKey(email))
	if err != nil || locked == "" {
		return false, 0
	}
	ttl, _ := r.TTL(ctx, accountLockKey(email))
	return true, ttl
}

// ── 3. Session Block Middleware ───────────────────────────────────────────────
//
// Applied AFTER AuthenticatedMiddleware. Reads session_id from gin context
// (set by the auth middleware when the JWT contains one) and rejects requests
// for force-blocked sessions.

func SessionBlockMiddleware(sm *SessionManager) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		sid, exists := ctx.Get("session_id")
		if !exists {
			// Old token without session support — backward compat.
			ctx.Next()
			return
		}

		sessionID, ok := sid.(string)
		if !ok || sessionID == "" {
			ctx.Next()
			return
		}

		meta, err := sm.loadSession(ctx.Request.Context(), sessionID)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized,
				basemodels.NewError("session expired — please log in again"))
			return
		}

		if meta.Blocked {
			ctx.AbortWithStatusJSON(http.StatusForbidden,
				basemodels.NewError("session has been revoked"))
			return
		}

		// Non-blocking heartbeat
		go sm.TouchSession(ctx.Request.Context(), sessionID)

		ctx.Next()
	}
}

// ── Key helpers (package-level, used by auth.go too) ─────────────────────────

func failedLoginKey(email string) string {
	return fmt.Sprintf("failed_login:%s", email)
}

func accountLockKey(email string) string {
	return fmt.Sprintf("account_lock:%s", email)
}

// ── Real-IP extraction ────────────────────────────────────────────────────────

// realIP resolves the true client IP behind Cloudflare / nginx reverse proxies.
func realIP(ctx *gin.Context) string {
	if xff := ctx.GetHeader("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := ctx.GetHeader("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return ctx.ClientIP()
}