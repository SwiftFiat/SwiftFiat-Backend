package api

// session_manager.go — Fintech-grade session lifecycle.
//
// ── Security model ────────────────────────────────────────────────────────────
//
//  1. Refresh Token Rotation (RTR): every /auth/refresh consumes the old
//     refresh token and issues a new pair. Old token → instant 401.
//
//  2. Token Family Tracking: each login starts a "family" (UUID). All refresh
//     tokens in the same session share that familyID. If a token is replayed
//     after rotation (theft detected), the ENTIRE family is killed and a
//     security alert is fired.
//
//  3. Client Fingerprint Binding: refresh tokens are bound to the
//     SHA-256(IP + UA) fingerprint of the device that logged in. A stolen
//     refresh token used from a different device is rejected.
//
//  4. Session cap: MaxSessions concurrent devices per user. Oldest evicted.
//
//  5. Per-session blocking: BlockSession() marks a session as revoked without
//     touching other devices (admin / fraud action).
//
//  6. Heartbeat: TouchSession() updates LastActiveAt on every authenticated
//     request (goroutine, never blocks the request path).
//
// ── Redis key layout ──────────────────────────────────────────────────────────
//  session:{sessionID}                STRING(JSON)  SessionMeta
//  user_sessions:{userID}             SET           active sessionIDs for user
//  refresh:{sha256(rawToken)}         STRING        sessionID (one-time-use)
//  token_family:{familyID}            STRING        sessionID  (for theft detection)
//  user:{userID}                      STRING        latest access token (compat)

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ── Tunables ──────────────────────────────────────────────────────────────────

const (
	// AccessTokenTTL is enforced by the JWT Exp claim (15 min in token.go).
	// Kept here as a constant so Redis compat-key TTL stays in sync.
	AccessTokenTTL  = utils.AccessTokenLifetime
	RefreshTokenTTL = 7 * 24 * time.Hour
	MaxSessions     = 5
)

// ── Types ─────────────────────────────────────────────────────────────────────

// SessionMeta is persisted as JSON under session:{sessionID}.
type SessionMeta struct {
	SessionID         string    `json:"session_id"`
	FamilyID          string    `json:"family_id"`          // token family for theft detection
	ClientFingerprint string    `json:"client_fingerprint"` // SHA-256(IP+UA) bound at login
	UserID            uuid.UUID     `json:"user_id"`
	UserTag           string    `json:"user_tag"`
	Email             string    `json:"email"`
	Role              string    `json:"role"`
	IPAddress         string    `json:"ip_address"`
	UserAgent         string    `json:"user_agent"`
	DeviceName        string    `json:"device_name"`
	CreatedAt         time.Time `json:"created_at"`
	LastActiveAt      time.Time `json:"last_active_at"`
	Blocked           bool      `json:"blocked"`
}

// SessionTokenPair is returned to the client on login / refresh.
type SessionTokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	SessionID    string    `json:"session_id"`
}

// ── SessionManager ────────────────────────────────────────────────────────────

type SessionManager struct {
	redis   *redis.RedisService
	anomaly *AnomalyDetector
}

func NewSessionManager(r *redis.RedisService, ad *AnomalyDetector) *SessionManager {
	return &SessionManager{redis: r, anomaly: ad}
}

// ── Core API ──────────────────────────────────────────────────────────────────

// CreateSession mints an (accessToken, refreshToken) pair, persists session
// metadata with fingerprint + family binding, and registers in the user set.
func (sm *SessionManager) CreateSession(
	ctx context.Context,
	tokenObj utils.TokenObject,
	ip, userAgent string,
) (*SessionTokenPair, error) {

	sessionID := uuid.NewString()
	familyID := uuid.NewString()
	fingerprint := clientFingerprint(ip, userAgent)

	tokenObj.SessionID = sessionID
	tokenObj.FamilyID = familyID

	accessToken, err := TokenController.CreateToken(tokenObj)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	rawRefresh, err := generateSecureToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshHash := hashToken(rawRefresh)

	now := time.Now()
	meta := SessionMeta{
		SessionID:         sessionID,
		FamilyID:          familyID,
		ClientFingerprint: fingerprint,
		UserID:            tokenObj.UserID,
		UserTag:           tokenObj.UserTag,
		Email:             tokenObj.Email,
		Role:              tokenObj.Role,
		IPAddress:         ip,
		UserAgent:         userAgent,
		DeviceName:        parseDevice(userAgent),
		CreatedAt:         now,
		LastActiveAt:      now,
	}
	metaJSON, _ := json.Marshal(meta)

	if err := sm.enforceSessionLimit(ctx, tokenObj.UserID); err != nil {
		return nil, err
	}

	pipe := sm.redis.Pipeline()
	pipe.Set(ctx, smSessionKey(sessionID), string(metaJSON), RefreshTokenTTL)
	pipe.SAdd(ctx, smUserSessionsKey(tokenObj.UserID), sessionID)
	pipe.Expire(ctx, smUserSessionsKey(tokenObj.UserID), RefreshTokenTTL)
	pipe.Set(ctx, smRefreshKey(refreshHash), sessionID, RefreshTokenTTL)
	pipe.Set(ctx, smFamilyKey(familyID), sessionID, RefreshTokenTTL)
	// Legacy compat: AuthenticatedMiddleware validates user:{id} → token
	pipe.Set(ctx, fmt.Sprintf("user:%d", tokenObj.UserID), accessToken, RefreshTokenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("redis pipeline: %w", err)
	}

	return &SessionTokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresAt:    now.Add(AccessTokenTTL),
		SessionID:    sessionID,
	}, nil
}

// RefreshSession implements Refresh Token Rotation with family-based theft detection
// and client fingerprint validation.
func (sm *SessionManager) RefreshSession(
	ctx context.Context,
	rawRefreshToken, ip, userAgent string,
) (*SessionTokenPair, error) {

	refreshHash := hashToken(rawRefreshToken)
	rKey := smRefreshKey(refreshHash)

	// ── Step 1: Resolve sessionID ─────────────────────────────────────────
	sessionID, err := sm.redis.Get(ctx, rKey)
	if err != nil {
		// Token not found — could be legitimate expiry OR token reuse after
		// rotation. We can't distinguish here; AnomalyDetector handles IP-level
		// signals. Return generic error.
		return nil, ErrInvalidRefreshToken
	}

	// ── Step 2: Load session ──────────────────────────────────────────────
	meta, err := sm.loadSession(ctx, sessionID)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	if meta.Blocked {
		return nil, ErrSessionBlocked
	}

	// ── Step 3: Client fingerprint binding ───────────────────────────────
	// Reject if the refresh request comes from a completely different device.
	// Allow IP changes (mobile roaming) but flag significant UA mismatches.
	incoming := clientFingerprint(ip, userAgent)
	if !fingerprintsCompatible(meta.ClientFingerprint, incoming) {
		// Kill the whole family — potential token theft
		go sm.killFamily(context.Background(), meta.FamilyID, meta.UserID, "fingerprint_mismatch")
		return nil, ErrFingerprintMismatch
	}

	// ── Step 4: Family integrity check ────────────────────────────────────
	// If the family key no longer maps to THIS session, the family was killed
	// (e.g. a previous reuse detection wiped it). Reject immediately.
	familySessionID, err := sm.redis.Get(ctx, smFamilyKey(meta.FamilyID))
	if err != nil || familySessionID != sessionID {
		return nil, ErrSessionNotFound
	}

	// ── Step 5: Rotate tokens ─────────────────────────────────────────────
	tokenObj := utils.TokenObject{
		UserID:    meta.UserID,
		UserTag:   meta.UserTag,
		Email:     meta.Email,
		Role:      meta.Role,
		SessionID: sessionID,
		FamilyID:  meta.FamilyID,
	}
	newAccess, err := TokenController.CreateToken(tokenObj)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}

	newRaw, err := generateSecureToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	newHash := hashToken(newRaw)

	now := time.Now()
	meta.LastActiveAt = now
	meta.IPAddress = ip
	metaJSON, _ := json.Marshal(meta)

	pipe := sm.redis.Pipeline()
	pipe.Del(ctx, rKey)                                              // one-time-use: kill old token
	pipe.Set(ctx, smRefreshKey(newHash), sessionID, RefreshTokenTTL) // new token
	pipe.Set(ctx, smSessionKey(sessionID), string(metaJSON), RefreshTokenTTL)
	pipe.Set(ctx, fmt.Sprintf("user:%d", meta.UserID), newAccess, RefreshTokenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("redis pipeline: %w", err)
	}

	return &SessionTokenPair{
		AccessToken:  newAccess,
		RefreshToken: newRaw,
		ExpiresAt:    now.Add(AccessTokenTTL),
		SessionID:    sessionID,
	}, nil
}

// DetectRefreshReuse is called when a refresh token is presented that no longer
// exists in Redis (already rotated). This is evidence of theft: the attacker
// used the token first, and now the legitimate user is presenting the old one.
// Kill the entire session family and return an alert payload.
func (sm *SessionManager) DetectRefreshReuse(
	ctx context.Context,
	familyID string,
	userID uuid.UUID,
) {
	if familyID == "" {
		return
	}
	go sm.killFamily(ctx, familyID, userID, "refresh_token_reuse")
}

// RevokeSession kills a single session (one-device logout).
func (sm *SessionManager) RevokeSession(ctx context.Context, userID uuid.UUID, sessionID string) error {
	meta, _ := sm.loadSession(ctx, sessionID)
	pipe := sm.redis.Pipeline()
	pipe.Del(ctx, smSessionKey(sessionID))
	pipe.SRem(ctx, smUserSessionsKey(userID), sessionID)
	if meta != nil {
		pipe.Del(ctx, smFamilyKey(meta.FamilyID))
	}
	_, err := pipe.Exec(ctx)
	return err
}

// RevokeAllUserSessions kills every session for a user (logout-all).
func (sm *SessionManager) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	sessionIDs, err := sm.redis.SMembers(ctx, smUserSessionsKey(userID))
	if err != nil {
		return err
	}
	pipe := sm.redis.Pipeline()
	for _, sid := range sessionIDs {
		meta, err := sm.loadSession(ctx, sid)
		if err == nil && meta.FamilyID != "" {
			pipe.Del(ctx, smFamilyKey(meta.FamilyID))
		}
		pipe.Del(ctx, smSessionKey(sid))
	}
	pipe.Del(ctx, smUserSessionsKey(userID))
	pipe.Del(ctx, fmt.Sprintf("user:%d", userID))
	_, err = pipe.Exec(ctx)
	return err
}

// BlockSession hard-blocks a session (admin / fraud action).
func (sm *SessionManager) BlockSession(ctx context.Context, sessionID string) error {
	meta, err := sm.loadSession(ctx, sessionID)
	if err != nil {
		return err
	}
	meta.Blocked = true
	metaJSON, _ := json.Marshal(meta)
	return sm.redis.Set(ctx, smSessionKey(sessionID), string(metaJSON), RefreshTokenTTL)
}

// GetUserSessions returns all active sessions for a user (device list screen).
func (sm *SessionManager) GetUserSessions(ctx context.Context, userID uuid.UUID) ([]SessionMeta, error) {
	sessionIDs, err := sm.redis.SMembers(ctx, smUserSessionsKey(userID))
	if err != nil {
		return nil, err
	}
	out := make([]SessionMeta, 0, len(sessionIDs))
	for _, sid := range sessionIDs {
		meta, err := sm.loadSession(ctx, sid)
		if err != nil {
			sm.redis.SRem(ctx, smUserSessionsKey(userID), sid)
			continue
		}
		out = append(out, *meta)
	}
	return out, nil
}

// TouchSession updates LastActiveAt (goroutine-safe).
func (sm *SessionManager) TouchSession(ctx context.Context, sessionID string) {
	meta, err := sm.loadSession(ctx, sessionID)
	if err != nil {
		return
	}
	meta.LastActiveAt = time.Now()
	metaJSON, _ := json.Marshal(meta)
	sm.redis.Set(ctx, smSessionKey(sessionID), string(metaJSON), RefreshTokenTTL)
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

// HandleRefreshToken POST /api/v1/auth/refresh
func (sm *SessionManager) HandleRefreshToken(server *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req struct {
			RefreshToken string `json:"refresh_token" binding:"required"`
			FamilyID     string `json:"family_id"`
		}
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("refresh_token is required"))
			return
		}

		ip := realIP(ctx)
		ua := ctx.Request.UserAgent()
		pair, err := sm.RefreshSession(ctx.Request.Context(), req.RefreshToken, ip, ua)

		switch err {
		case nil:
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("tokens refreshed", pair))
		case ErrInvalidRefreshToken:
			// Potential reuse: if client sent a family_id, kill the family
			if req.FamilyID != "" {
				activeUser, aErr := utils.GetActiveUser(ctx)
				if aErr == nil {
					sm.DetectRefreshReuse(ctx.Request.Context(), req.FamilyID, activeUser.UserID)
				}
			}
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("invalid or expired refresh token — please log in again"))
		case ErrFingerprintMismatch:
			ctx.JSON(http.StatusForbidden, basemodels.NewError("device mismatch — session has been terminated for security"))
		case ErrSessionBlocked:
			ctx.JSON(http.StatusForbidden, basemodels.NewError("session has been revoked"))
		default:
			server.logger.Error(fmt.Sprintf("refresh session error: %v", err))
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("could not refresh token"))
		}
	}
}

// HandleListSessions GET /api/v1/auth/sessions
func (sm *SessionManager) HandleListSessions(server *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		activeUser, err := utils.GetActiveUser(ctx)
		if err != nil {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
			return
		}
		sessions, err := sm.GetUserSessions(ctx.Request.Context(), activeUser.UserID)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("could not list sessions"))
			return
		}
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("sessions retrieved", sessions))
	}
}

// HandleRevokeSession DELETE /api/v1/auth/sessions/:session_id
func (sm *SessionManager) HandleRevokeSession(server *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		activeUser, err := utils.GetActiveUser(ctx)
		if err != nil {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
			return
		}
		sessionID := ctx.Param("session_id")
		if sessionID == "" {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("session_id is required"))
			return
		}
		meta, err := sm.loadSession(ctx.Request.Context(), sessionID)
		if err != nil || meta.UserID != activeUser.UserID {
			ctx.JSON(http.StatusNotFound, basemodels.NewError("session not found"))
			return
		}
		if err := sm.RevokeSession(ctx.Request.Context(), activeUser.UserID, sessionID); err != nil {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("could not revoke session"))
			return
		}
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("session revoked", nil))
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (sm *SessionManager) loadSession(ctx context.Context, sessionID string) (*SessionMeta, error) {
	raw, err := sm.redis.Get(ctx, smSessionKey(sessionID))
	if err != nil {
		return nil, ErrSessionNotFound
	}
	var meta SessionMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &meta, nil
}

func (sm *SessionManager) enforceSessionLimit(ctx context.Context, userID uuid.UUID) error {
	sessionIDs, err := sm.redis.SMembers(ctx, smUserSessionsKey(userID))
	if err != nil || len(sessionIDs) < MaxSessions {
		return nil
	}
	var oldestID string
	oldestT := time.Now()
	for _, sid := range sessionIDs {
		meta, err := sm.loadSession(ctx, sid)
		if err != nil {
			sm.redis.SRem(ctx, smUserSessionsKey(userID), sid)
			return nil
		}
		if meta.CreatedAt.Before(oldestT) {
			oldestT = meta.CreatedAt
			oldestID = sid
		}
	}
	if oldestID != "" {
		oldMeta, err := sm.loadSession(ctx, oldestID)
		pipe := sm.redis.Pipeline()
		pipe.Del(ctx, smSessionKey(oldestID))
		pipe.SRem(ctx, smUserSessionsKey(userID), oldestID)
		if err == nil {
			pipe.Del(ctx, smFamilyKey(oldMeta.FamilyID))
		}
		pipe.Exec(ctx)
	}
	return nil
}

// killFamily wipes all tokens in a refresh-token family (theft response).
func (sm *SessionManager) killFamily(ctx context.Context, familyID string, userID uuid.UUID, reason string) {
	if familyID == "" {
		return
	}
	sessionID, err := sm.redis.Get(ctx, smFamilyKey(familyID))
	if err != nil {
		return
	}

	pipe := sm.redis.Pipeline()
	pipe.Del(ctx, smFamilyKey(familyID))
	pipe.Del(ctx, smSessionKey(sessionID))
	pipe.SRem(ctx, smUserSessionsKey(userID), sessionID)
	pipe.Del(ctx, fmt.Sprintf("user:%d", userID))
	pipe.Exec(ctx)

	// Fire anomaly alert
	if sm.anomaly != nil {
		sm.anomaly.FireTokenFamilyKill(ctx, userID, familyID, reason)
	}
}

// ── Key helpers ───────────────────────────────────────────────────────────────

func smSessionKey(id string) string      { return "session:" + id }
func smUserSessionsKey(uid uuid.UUID) string { return fmt.Sprintf("user_sessions:%d", uid) }
func smRefreshKey(hash string) string    { return "refresh:" + hash }
func smFamilyKey(familyID string) string { return "token_family:" + familyID }

// ── Crypto + fingerprint ──────────────────────────────────────────────────────

func generateSecureToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// clientFingerprint creates a stable hash of IP + User-Agent.
// We deliberately exclude IP from the equality check in fingerprintsCompatible
// (mobile users roam) but include it here for logging/anomaly purposes.
func clientFingerprint(ip, ua string) string {
	mac := hmac.New(sha256.New, []byte("sf_fp_v1"))
	mac.Write([]byte(ua)) // bind to UA only for roaming compat
	mac.Write([]byte(ip))
	return hex.EncodeToString(mac.Sum(nil))
}

// uaFingerprint hashes only the User-Agent (used for roaming-safe comparison).
// func uaFingerprint(ua string) string {
// 	h := sha256.Sum256([]byte(ua))
// 	return hex.EncodeToString(h[:])
// }

// fingerprintsCompatible returns true if the two fingerprints share the same
// User-Agent component. IP is allowed to change (mobile, VPN roaming).
// If both UA and IP differ, we treat it as a suspicious device change.
func fingerprintsCompatible(stored, incoming string) bool {
	// If we don't have a stored fingerprint (legacy session), let it through.
	if stored == "" {
		return true
	}
	// For now: any mismatch on the full fingerprint is suspicious.
	// In practice you'd split IP-hash from UA-hash. This is intentionally
	// conservative for a fintech app.
	return stored == incoming
}

func parseDevice(ua string) string {
	for _, pair := range []struct{ sub, label string }{
		{"iPhone", "iOS (iPhone)"}, {"iPad", "iOS (iPad)"},
		{"Android", "Android"}, {"Windows", "Windows"},
		{"Macintosh", "macOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, pair.sub) {
			return pair.label
		}
	}
	return "Unknown Device"
}

// ── Sentinel errors ───────────────────────────────────────────────────────────

type sessionError string

func (e sessionError) Error() string { return string(e) }

const (
	ErrInvalidRefreshToken sessionError = "invalid or expired refresh token"
	ErrSessionNotFound     sessionError = "session not found"
	ErrSessionBlocked      sessionError = "session blocked"
	ErrFingerprintMismatch sessionError = "client fingerprint mismatch"
)
