package api

// otp_guard.go — OTP brute-force protection for SwiftFiat.
//
// A 6-digit OTP has 1,000,000 possibilities. Without a per-OTP attempt counter,
// an attacker can brute-force it within any rate-limit window that allows more
// than ~30 attempts. This file enforces a 5-guess maximum per OTP issuance,
// after which the OTP is invalidated and must be re-requested.
//
// ── Redis key layout ──────────────────────────────────────────────────────────
//  otp_attempts:{scope}:{identifier}   STRING  attempt counter (TTL = OTP TTL)
//
// Scopes: "email_verify", "password_reset", "admin_otp", "2fa_email"

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/gin-gonic/gin"
)

const (
	OTPMaxAttempts = 5
	// OTPAttemptTTL is the TTL for the attempt counter — set to match the OTP's
	// own TTL so they expire together. Pass the OTP TTL when calling RecordOTPAttempt.
	OTPAttemptTTL = 10 * time.Minute
)

// OTPScope identifies which OTP issuance context we're protecting.
type OTPScope string

const (
	OTPScopeEmailVerify   OTPScope = "email_verify"
	OTPScopePasswordReset OTPScope = "password_reset"
	OTPScopeAdminLogin    OTPScope = "admin_otp"
	OTPScopeEmailCode2FA  OTPScope = "2fa_email"
)

// ── OTPGuard: stateless helpers used by auth.go handlers ─────────────────────

// RecordOTPAttempt increments the attempt counter for the given scope+identifier
// (e.g. scope=email_verify, id=user@example.com).
//
// Returns:
//   - blocked=true  when the counter ≥ OTPMaxAttempts (OTP invalidated)
//   - remaining     how many guesses are left
//
// On the first increment the TTL is set to `otpTTL` so the counter expires
// exactly when the OTP expires.
func RecordOTPAttempt(
	ctx context.Context,
	r *redis.RedisService,
	scope OTPScope,
	identifier string,
	otpTTL time.Duration,
) (blocked bool, remaining int) {

	key := otpAttemptsKey(scope, identifier)
	count, err := r.Incr(ctx, key)
	if err != nil {
		return false, OTPMaxAttempts
	}
	if count == 1 {
		r.Expire(ctx, key, otpTTL)
	}

	left := OTPMaxAttempts - int(count)
	if left < 0 {
		left = 0
	}

	if count > OTPMaxAttempts {
		return true, 0
	}

	return false, left
}

// InvalidateOTPAfterBlock wipes both the OTP value and the attempt counter
// once the max is reached, preventing further guesses even after a reset.
func InvalidateOTPAfterBlock(
	ctx context.Context,
	r *redis.RedisService,
	scope OTPScope,
	identifier string,
	otpRedisKey string, // e.g. "password_reset_otp:user@example.com"
) {
	pipe := r.Pipeline()
	pipe.Del(ctx, otpAttemptsKey(scope, identifier))
	pipe.Del(ctx, otpRedisKey)
	pipe.Exec(ctx)
}

// ClearOTPAttempts resets the counter on successful verification.
func ClearOTPAttempts(ctx context.Context, r *redis.RedisService, scope OTPScope, identifier string) {
	r.Delete(ctx, otpAttemptsKey(scope, identifier))
}

// GetOTPAttemptCount returns the current attempt count (0 if key missing).
func GetOTPAttemptCount(ctx context.Context, r *redis.RedisService, scope OTPScope, identifier string) int {
	val, err := r.Get(ctx, otpAttemptsKey(scope, identifier))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(val)
	return n
}

// OTPGuardMiddleware is a Gin middleware factory that wraps an OTP verification
// endpoint. It enforces OTPMaxAttempts before the handler runs, so the handler
// doesn't need to think about brute force at all.
//
// Usage:
//
//	router.POST("verify-otp",
//	    OTPGuardMiddleware(redis, OTPScopePasswordReset, func(ctx *gin.Context) string {
//	        var req struct{ Email string `json:"email"` }
//	        ctx.ShouldBindJSON(&req)
//	        return req.Email
//	    }),
//	    handler.VerifyOTP,
//	)
func OTPGuardMiddleware(
	r *redis.RedisService,
	scope OTPScope,
	identifierFn func(*gin.Context) string, // extracts the key from the request
) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identifier := identifierFn(ctx)
		if identifier == "" {
			ctx.Next()
			return
		}

		count := GetOTPAttemptCount(ctx.Request.Context(), r, scope, identifier)
		if count >= OTPMaxAttempts {
			ctx.AbortWithStatusJSON(http.StatusTooManyRequests, basemodels.NewError(
				"too many incorrect OTP attempts — please request a new OTP"))
			return
		}

		ctx.Next()
	}
}

// CheckOTPWithGuard is a helper called inside auth handlers to do the full
// OTP check + attempt tracking in one call.
//
// Returns (valid bool, httpStatus int, errMsg string).
// On invalid OTP it automatically increments the counter and invalidates the
// OTP key if the max is reached.
func CheckOTPWithGuard(
	ctx context.Context,
	r *redis.RedisService,
	scope OTPScope,
	identifier string,
	otpRedisKey string, // Redis key holding the expected OTP value
	providedCode string,
	expectedCode string,
	otpTTL time.Duration,
) (valid bool, httpStatus int, errMsg string) {

	if expectedCode == "" {
		return false, http.StatusBadRequest, "OTP not found or already expired — please request a new one"
	}

	if providedCode != expectedCode {
		blocked, remaining := RecordOTPAttempt(ctx, r, scope, identifier, otpTTL)
		if blocked {
			InvalidateOTPAfterBlock(ctx, r, scope, identifier, otpRedisKey)
			return false, http.StatusTooManyRequests,
				"maximum OTP attempts exceeded — your OTP has been invalidated, please request a new one"
		}
		return false, http.StatusBadRequest,
			fmt.Sprintf("invalid OTP — %d attempt(s) remaining before invalidation", remaining)
	}

	// Success — clear attempt counter
	ClearOTPAttempts(ctx, r, scope, identifier)
	return true, http.StatusOK, ""
}

// ── Key helpers ───────────────────────────────────────────────────────────────

func otpAttemptsKey(scope OTPScope, identifier string) string {
	return fmt.Sprintf("otp_attempts:%s:%s", scope, identifier)
}
